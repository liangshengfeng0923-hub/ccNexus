package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/storage"
)

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
}

// Usage represents token usage information from API response
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// APIResponse represents the structure of API responses to extract usage
type APIResponse struct {
	Usage Usage `json:"usage"`
}

// Proxy represents the proxy server
type Proxy struct {
	config            *config.Config
	storage           *storage.SQLiteStorage
	stats             *Stats
	currentIndex      int
	mu                sync.RWMutex
	server            *http.Server
	httpClient        *http.Client                  // Reusable HTTP client with connection pool
	activeRequests    map[string]bool               // tracks active requests by endpoint name
	activeRequestsMu  sync.RWMutex                  // protects activeRequests map
	endpointCtx       map[string]context.Context    // context per endpoint for cancellation
	endpointCancel    map[string]context.CancelFunc // cancel functions per endpoint
	ctxMu             sync.RWMutex                  // protects context maps
	onEndpointSuccess func(endpointName string)     // callback when endpoint request succeeds
	modelsCache       *ModelsCache                  // Cache for /v1/models endpoint
	resolver          *EndpointResolver             // 端点解析器，用于解析客户端指定的端点
}

// New creates a new Proxy instance
func New(cfg *config.Config, statsStorage StatsStorage, sqliteStorage *storage.SQLiteStorage, deviceID string) *Proxy {
	stats := NewStats(statsStorage, deviceID)

	// Create a reusable HTTP client with connection pool
	// Enhanced configuration for large SSE streaming and HTTP/2 support
	httpClient := &http.Client{
		Timeout: 300 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:           100,
			MaxIdleConnsPerHost:    10,
			IdleConnTimeout:        90 * time.Second,
			TLSHandshakeTimeout:    10 * time.Second,
			ExpectContinueTimeout:  1 * time.Second,
			ResponseHeaderTimeout:  90 * time.Second,
			WriteBufferSize:        128 * 1024, // 128KB write buffer for large SSE streams
			ReadBufferSize:         128 * 1024, // 128KB read buffer for large SSE streams
			MaxResponseHeaderBytes: 64 * 1024,  // 64KB max response headers
		},
	}

	return &Proxy{
		config:         cfg,
		storage:        sqliteStorage,
		stats:          stats,
		currentIndex:   0,
		httpClient:     httpClient,
		activeRequests: make(map[string]bool),
		endpointCtx:    make(map[string]context.Context),
		endpointCancel: make(map[string]context.CancelFunc),
		modelsCache:    NewModelsCache(cfg.ModelsCacheTTL),
		resolver:       NewEndpointResolverWithFunc(cfg.GetEndpoints),
	}
}

// SetOnEndpointSuccess sets the callback for successful endpoint requests
func (p *Proxy) SetOnEndpointSuccess(callback func(endpointName string)) {
	p.onEndpointSuccess = callback
}

// Start starts the proxy server
func (p *Proxy) Start() error {
	return p.StartWithMux(nil)
}

// StartWithMux starts the proxy server with an optional custom mux
func (p *Proxy) StartWithMux(customMux *http.ServeMux) error {
	port := p.config.GetPort()

	var mux *http.ServeMux
	if customMux != nil {
		mux = customMux
	} else {
		mux = http.NewServeMux()
	}

	// Register proxy routes
	mux.HandleFunc("/", p.handleProxy)
	mux.HandleFunc("/v1/messages/count_tokens", p.handleCountTokens)
	mux.HandleFunc("/v1/models", p.handleModels)
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/stats", p.handleStats)

	p.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	logger.Info("ccNexus starting on port %d", port)
	logger.Info("Configured %d endpoints", len(p.config.GetEndpoints()))

	return p.server.ListenAndServe()
}

// Stop stops the proxy server
func (p *Proxy) Stop() error {
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

// getEnabledEndpoints returns only the enabled endpoints
func (p *Proxy) getEnabledEndpoints() []config.Endpoint {
	allEndpoints := p.config.GetEndpoints()
	enabled := make([]config.Endpoint, 0)
	for _, ep := range allEndpoints {
		if ep.Enabled {
			enabled = append(enabled, ep)
		}
	}
	return enabled
}

// getCurrentEndpoint returns the current endpoint (thread-safe)
func (p *Proxy) getCurrentEndpoint() config.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		// Return empty endpoint if no enabled endpoints
		return config.Endpoint{}
	}
	// Make sure currentIndex is within bounds
	index := p.currentIndex % len(endpoints)
	return endpoints[index]
}

// markRequestActive marks an endpoint as having active requests
func (p *Proxy) markRequestActive(endpointName string) {
	p.activeRequestsMu.Lock()
	defer p.activeRequestsMu.Unlock()
	p.activeRequests[endpointName] = true
}

// markRequestInactive marks an endpoint as having no active requests
func (p *Proxy) markRequestInactive(endpointName string) {
	p.activeRequestsMu.Lock()
	defer p.activeRequestsMu.Unlock()
	delete(p.activeRequests, endpointName)
}

// hasActiveRequests checks if an endpoint has active requests
func (p *Proxy) hasActiveRequests(endpointName string) bool {
	p.activeRequestsMu.RLock()
	defer p.activeRequestsMu.RUnlock()
	return p.activeRequests[endpointName]
}

// isCurrentEndpoint checks if the given endpoint is still the current one
func (p *Proxy) isCurrentEndpoint(endpointName string) bool {
	current := p.getCurrentEndpoint()
	return current.Name == endpointName
}

// getEndpointContext returns a context for the given endpoint, creating one if needed
func (p *Proxy) getEndpointContext(endpointName string) context.Context {
	p.ctxMu.Lock()
	defer p.ctxMu.Unlock()

	if ctx, ok := p.endpointCtx[endpointName]; ok {
		return ctx
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.endpointCtx[endpointName] = ctx
	p.endpointCancel[endpointName] = cancel
	return ctx
}

// cancelEndpointRequests cancels all requests for the given endpoint
func (p *Proxy) cancelEndpointRequests(endpointName string) {
	p.ctxMu.Lock()
	defer p.ctxMu.Unlock()

	if cancel, ok := p.endpointCancel[endpointName]; ok {
		cancel()
		delete(p.endpointCtx, endpointName)
		delete(p.endpointCancel, endpointName)
	}
}

// rotateEndpoint switches to the next endpoint (thread-safe)
// waitForActive: if true, waits briefly for active requests to complete before switching
func (p *Proxy) rotateEndpoint() config.Endpoint {
	// First, check if we need to wait for active requests
	oldEndpoint := p.getCurrentEndpoint()
	if p.hasActiveRequests(oldEndpoint.Name) {
		logger.Debug("[SWITCH] Waiting for active requests on %s to complete...", oldEndpoint.Name)

		// Wait outside of the main lock to avoid blocking other operations
		for i := 0; i < 10; i++ { // Check 10 times, 50ms each = 500ms max
			time.Sleep(50 * time.Millisecond)
			if !p.hasActiveRequests(oldEndpoint.Name) {
				break
			}
		}
	}

	// Now acquire lock and perform the rotation
	p.mu.Lock()
	defer p.mu.Unlock()

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		return config.Endpoint{}
	}

	oldIndex := p.currentIndex % len(endpoints)
	oldEndpoint = endpoints[oldIndex]

	// Calculate next index
	p.currentIndex = (oldIndex + 1) % len(endpoints)

	newEndpoint := endpoints[p.currentIndex]
	if len(endpoints) > 1 && oldEndpoint.Name != newEndpoint.Name {
		logger.Debug("[SWITCH] %s → %s (#%d)", oldEndpoint.Name, newEndpoint.Name, p.currentIndex+1)
	}

	return newEndpoint
}

// GetCurrentEndpointName returns the current endpoint name (thread-safe)
func (p *Proxy) GetCurrentEndpointName() string {
	endpoint := p.getCurrentEndpoint()
	return endpoint.Name
}

// SetCurrentEndpoint manually switches to a specific endpoint by name
// Returns error if endpoint not found or not enabled
// Thread-safe and cancels ongoing requests on the old endpoint
func (p *Proxy) SetCurrentEndpoint(targetName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no enabled endpoints")
	}

	// Find the endpoint by name
	for i, ep := range endpoints {
		if ep.Name == targetName {
			oldEndpoint := endpoints[p.currentIndex%len(endpoints)]
			if oldEndpoint.Name != targetName {
				// Cancel all requests on the old endpoint
				p.cancelEndpointRequests(oldEndpoint.Name)
			}
			p.currentIndex = i
			logger.Info("[MANUAL SWITCH] %s → %s", oldEndpoint.Name, ep.Name)
			return nil
		}
	}

	return fmt.Errorf("endpoint '%s' not found or not enabled", targetName)
}

// ClientFormat represents the API format used by the client
type ClientFormat string

const (
	ClientFormatClaude          ClientFormat = "claude"           // Claude Code: /v1/messages
	ClientFormatOpenAIChat      ClientFormat = "openai_chat"      // Codex (chat): /v1/chat/completions
	ClientFormatOpenAIResponses ClientFormat = "openai_responses" // Codex (responses): /v1/responses
)

// detectClientFormat identifies the client format based on request path
func detectClientFormat(path string) ClientFormat {
	switch {
	case strings.HasPrefix(path, "/v1/chat/completions") || strings.HasPrefix(path, "/chat/completions"):
		return ClientFormatOpenAIChat
	case strings.HasPrefix(path, "/v1/responses") || strings.HasPrefix(path, "/responses"):
		return ClientFormatOpenAIResponses
	default:
		return ClientFormatClaude
	}
}

// handleProxy handles the main proxy logic
func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// API Key 验证（在读取请求体之后进行，以便确定目标端点）
	if p.config.APIKeyAuthEnabled {
		if !p.__validateAPIKey(r, w, bodyBytes) {
			return
		}
	}

	requestStart := time.Now()
	reqBytes := len(bodyBytes)

	// Detect client format
	clientFormat := detectClientFormat(r.URL.Path)

	logger.DebugLog("=== Proxy Request ===")
	logger.DebugLog("Method: %s, Path: %s, ClientFormat: %s", r.Method, r.URL.Path, clientFormat)
	logger.DebugLog("Request Body: %s", string(bodyBytes))

	var streamReq struct {
		Model    string      `json:"model"`
		Thinking interface{} `json:"thinking"`
		Stream   bool        `json:"stream"`
	}
	json.Unmarshal(bodyBytes, &streamReq)

	// 在解析时记录原始模型名称，用于后续处理
	// originalModelName := strings.TrimSpace(streamReq.Model)

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		logger.Error("No enabled endpoints available")
		http.Error(w, "No enabled endpoints configured", http.StatusServiceUnavailable)
		return
	}

	// 尝试解析客户端指定的端点
	specifiedEndpoint, modelOverride, resolveErr := p.resolver.ResolveEndpoint(r, bodyBytes)

	// 检查是否有 API Key 验证时自动选择的端点
	autoSelectedEndpoint := r.Context().Value("selectedEndpoint")
	if autoSelectedEndpoint != nil {
		if ep, ok := autoSelectedEndpoint.(*config.Endpoint); ok {
			logger.Debug("[API Key Auth] Using auto-selected endpoint from context: %s (model: %s)", ep.Name, ep.Model)
			specifiedEndpoint = ep
			// 自动选择端点时，使用端点配置的模型
			if ep.Model != "" {
				modelOverride = ep.Model
				logger.Debug("[API Key Auth] Override model to: %s", modelOverride)
			}
		}
	}

	if resolveErr != nil && specifiedEndpoint == nil {
		// 端点指定错误，返回错误响应
		logger.Warn("端点解析失败: %v", resolveErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errorResp := map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "invalid_request_error",
				"message": resolveErr.Error(),
			},
		}
		if jsonBytes, err := json.Marshal(errorResp); err == nil {
			w.Write(jsonBytes)
		}
		return
	}

	// 如果指定了端点，使用该端点；否则使用轮询机制
	var useSpecificEndpoint bool
	if specifiedEndpoint != nil {
		useSpecificEndpoint = true
		logger.Debug("[Resolver] Using specified endpoint: %s (auto-selected=%v)",
			specifiedEndpoint.Name, autoSelectedEndpoint != nil)
	}

	maxRetries := p.computeMaxRetries(endpoints)
	endpointAttempts := 0
	lastEndpointName := ""
	refreshedCredentialAttempts := make(map[int64]bool)

	for retry := 0; retry < maxRetries; retry++ {
		var endpoint config.Endpoint
		if useSpecificEndpoint {
			// 使用指定的端点，不进行轮询
			endpoint = *specifiedEndpoint
		} else {
			// 使用轮询机制
			endpoint = p.getCurrentEndpoint()
		}

		if endpoint.Name == "" {
			http.Error(w, "No enabled endpoints available", http.StatusServiceUnavailable)
			return
		}

		// Reset attempts counter if endpoint changed (e.g., manual switch)
		if lastEndpointName != "" && lastEndpointName != endpoint.Name {
			endpointAttempts = 0
		}
		lastEndpointName = endpoint.Name

		endpointAttempts++
		p.markRequestActive(endpoint.Name)

		authMode := config.NormalizeAuthMode(endpoint.AuthMode)
		apiKey := strings.TrimSpace(endpoint.APIKey)
		credentialID := int64(0)
		var selectedCredential *storage.EndpointCredential
		if config.IsTokenPoolAuthMode(authMode) {
			credential, err := p.selectCredential(endpoint.Name)
			if err != nil {
				logger.Warn("[%s] Failed to select token pool credential: %v", endpoint.Name, err)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				if endpointAttempts >= 2 && !useSpecificEndpoint {
					p.rotateEndpoint()
					endpointAttempts = 0
				}
				continue
			}
			if credential == nil || strings.TrimSpace(credential.AccessToken) == "" {
				logger.Warn("[%s] No usable token in token pool", endpoint.Name)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				if endpointAttempts >= 2 && !useSpecificEndpoint {
					p.rotateEndpoint()
					endpointAttempts = 0
				}
				continue
			}
			selectedCredential = credential
			if shouldTryCredentialRefresh(credential, time.Now().UTC()) {
				refreshed, refreshErr := p.refreshCredential(endpoint, credential)
				if refreshErr != nil {
					logger.Warn("[%s] Preflight credential refresh failed (id=%d): %v", endpoint.Name, credential.ID, refreshErr)
				} else {
					selectedCredential = refreshed
					refreshedCredentialAttempts[refreshed.ID] = true
				}
			}
			apiKey = strings.TrimSpace(credential.AccessToken)
			if selectedCredential != nil {
				apiKey = strings.TrimSpace(selectedCredential.AccessToken)
				credentialID = selectedCredential.ID
			}
		} else if apiKey == "" {
			logger.Warn("[%s] API key mode but apiKey is empty", endpoint.Name)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		trans, err := prepareTransformerForClient(clientFormat, endpoint)
		if err != nil {
			logger.Error("[%s] %v", endpoint.Name, err)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		transformerName := trans.Name()

		transformedBody, err := trans.TransformRequest(bodyBytes)
		if err != nil {
			logger.Error("[%s] Failed to transform request: %v", endpoint.Name, err)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		logger.DebugLog("[%s] Transformer: %s", endpoint.Name, transformerName)
		logger.DebugLog("[%s] Transformed Request: %s", endpoint.Name, string(transformedBody))

		// 如果有模型覆盖值，应用到转换后的请求体中
		if modelOverride != "" {
			transformedBody = overrideModelInPayload(transformedBody, modelOverride)
			logger.DebugLog("[%s] 应用模型覆盖后的请求: %s", endpoint.Name, string(transformedBody))
		}

		cleanedBody, err := cleanIncompleteToolCalls(transformedBody)
		if err != nil {
			logger.Warn("[%s] Failed to clean tool calls: %v", endpoint.Name, err)
			cleanedBody = transformedBody
		}
		transformedBody = cleanedBody
		if config.NormalizeAuthMode(endpoint.AuthMode) == config.AuthModeCodexTokenPool {
			transformedBody = overrideModelInPayload(transformedBody, endpoint.Model)
		}

		// 处理模型名称：优先使用模型覆盖值，然后是请求中的模型，最后是端点配置的模型
		modelName := strings.TrimSpace(streamReq.Model)
		if modelOverride != "" {
			// 使用解析器提供的模型覆盖值
			modelName = modelOverride
			logger.Debug("[%s] 使用模型覆盖值: %s", endpoint.Name, modelName)
		} else if modelName == "" || (authMode == config.AuthModeCodexTokenPool && strings.TrimSpace(endpoint.Model) != "") {
			modelName = endpoint.Model
		}

		var thinkingEnabled bool
		if strings.Contains(transformerName, "openai") {
			var openaiReq map[string]interface{}
			if err := json.Unmarshal(transformedBody, &openaiReq); err == nil {
				if enable, ok := openaiReq["enable_thinking"].(bool); ok {
					thinkingEnabled = enable
				}
			}
		}

		proxyReq, err := buildProxyRequest(r, endpoint, apiKey, transformedBody, transformerName, selectedCredential)
		if err != nil {
			logger.Error("[%s] Failed to create request: %v", endpoint.Name, err)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		proxyURL := resolveProxyURLForRequest(p.config, proxyReq.URL)
		proxyLabel := strings.TrimSpace(proxyURL)
		if streamReq.Stream {
			if proxyLabel == "" {
				logger.Debug("[%s] Streaming %s %d", endpoint.Name, modelName, reqBytes)
			} else {
				logger.Debug("[%s] Streaming %s %d %s", endpoint.Name, modelName, reqBytes, proxyLabel)
			}
		} else {
			if proxyLabel == "" {
				logger.Debug("[%s] Requesting %s %d", endpoint.Name, modelName, reqBytes)
			} else {
				logger.Debug("[%s] Requesting %s %d %s", endpoint.Name, modelName, reqBytes, proxyLabel)
			}
		}

		ctx := p.getEndpointContext(endpoint.Name)
		resp, err := sendRequest(ctx, proxyReq, p.httpClient, p.config)
		if err != nil {
			logger.Error("[%s] Request failed: %v", endpoint.Name, err)
			if isTransientNetworkError(err) {
				logger.Warn("[%s] Transient network error, retrying same endpoint: %v", endpoint.Name, err)
				p.markRequestInactive(endpoint.Name)
				time.Sleep(300 * time.Millisecond)
				endpointAttempts = 0
				continue
			}
			p.markCredentialFailure(credentialID, 0, err.Error())
			p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			p.captureCodexRateLimitsFromHeaders(endpoint, credentialID, resp.Header)
		}

		contentType := resp.Header.Get("Content-Type")
		isStreaming := shouldHandleAsStreamingResponse(contentType, streamReq.Stream, endpoint, transformerName)

		// Codex backend enforces stream=true upstream for /responses in some environments.
		// Bridge to non-stream client responses regardless of upstream Content-Type quirks.
		if resp.StatusCode == http.StatusOK && !streamReq.Stream && shouldAggregateCodexStreaming(endpoint, transformerName) {
			inputTokens, outputTokens, outputText, err := p.handleStreamingAsNonStreaming(w, resp, endpoint, trans, credentialID)
			if err == nil {
				// Fallback: estimate tokens when usage is missing.
				if inputTokens == 0 || outputTokens == 0 {
					inputTokens, outputTokens = p.estimateTokens(bodyBytes, outputText, inputTokens, outputTokens, endpoint.Name)
				}

				p.stats.RecordRequest(endpoint.Name)
				p.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
				p.recordCredentialUsage(credentialID, endpoint.Name, 1, 0, inputTokens, outputTokens)
				p.markCredentialSuccess(credentialID)
				p.markRequestInactive(endpoint.Name)
				if p.onEndpointSuccess != nil {
					p.onEndpointSuccess(endpoint.Name)
				}
				totalElapsed := time.Since(requestStart).Round(time.Millisecond)
				logger.Debug("[%s] Requested tokens=%d/%d latency=%s cred_id=%d", endpoint.Name, inputTokens, outputTokens, totalElapsed, credentialID)
				return
			}
			logger.Warn("[%s] Failed to aggregate streaming response as non-stream: %v", endpoint.Name, err)
			p.markCredentialFailure(credentialID, 0, err.Error())
			p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		if resp.StatusCode == http.StatusOK && isStreaming {
			inputTokens, outputTokens, outputText := p.handleStreamingResponse(w, resp, endpoint, trans, transformerName, thinkingEnabled, modelName, bodyBytes, credentialID)

			// Fallback: estimate tokens when usage is 0
			if inputTokens == 0 || outputTokens == 0 {
				inputTokens, outputTokens = p.estimateTokens(bodyBytes, outputText, inputTokens, outputTokens, endpoint.Name)
			}

			p.stats.RecordRequest(endpoint.Name)
			p.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
			p.recordCredentialUsage(credentialID, endpoint.Name, 1, 0, inputTokens, outputTokens)
			p.markCredentialSuccess(credentialID)
			p.markRequestInactive(endpoint.Name)
			if p.onEndpointSuccess != nil {
				p.onEndpointSuccess(endpoint.Name)
			}
			totalElapsed := time.Since(requestStart).Round(time.Millisecond)
			logger.Debug("[%s] Requested tokens=%d/%d latency=%s cred_id=%d", endpoint.Name, inputTokens, outputTokens, totalElapsed, credentialID)
			return
		}

		if resp.StatusCode == http.StatusOK {
			inputTokens, outputTokens, err := p.handleNonStreamingResponse(w, resp, endpoint, trans)
			if err == nil {
				p.stats.RecordRequest(endpoint.Name)
				p.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
				p.recordCredentialUsage(credentialID, endpoint.Name, 1, 0, inputTokens, outputTokens)
				p.markCredentialSuccess(credentialID)
			p.markRequestInactive(endpoint.Name)
			if p.onEndpointSuccess != nil {
				p.onEndpointSuccess(endpoint.Name)
			}
			totalElapsed := time.Since(requestStart).Round(time.Millisecond)
			logger.Debug("[%s] Requested tokens=%d/%d latency=%s cred_id=%d", endpoint.Name, inputTokens, outputTokens, totalElapsed, credentialID)
			return
		}
		}

		if shouldRetry(resp.StatusCode) {
			var errBody []byte
			if resp.Header.Get("Content-Encoding") == "gzip" {
				errBody, _ = decompressGzip(resp.Body)
			} else {
				errBody, _ = io.ReadAll(resp.Body)
			}
			resp.Body.Close()
			errMsg := string(errBody)
			if len(errMsg) > 200 {
				errMsg = errMsg[:200] + "..."
			}
			logger.Warn("[%s] Request failed %d: %s", endpoint.Name, resp.StatusCode, errMsg)
			logger.DebugLog("[%s] Request failed %d: %s", endpoint.Name, resp.StatusCode, errMsg)
			p.markCredentialFailure(credentialID, resp.StatusCode, errMsg)
			p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		var respBody []byte
		if resp.Header.Get("Content-Encoding") == "gzip" {
			respBody, _ = decompressGzip(resp.Body)
		} else {
			respBody, _ = io.ReadAll(resp.Body)
		}
		resp.Body.Close()
		skipCredentialPenalty := false

		// Token pool mode: on 401/403, invalidate current credential and retry within the same endpoint.
		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && credentialID > 0 {
			errMsg := string(respBody)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
			if !shouldTreatCredentialAuthFailure(resp.StatusCode, errMsg) {
				skipCredentialPenalty = true
				logger.Warn("[%s] Upstream %d looks like route/gateway denial, skipping credential invalidation", endpoint.Name, resp.StatusCode)
			}
			if skipCredentialPenalty {
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
			} else {
				if selectedCredential != nil &&
					isCodexProviderType(selectedCredential.ProviderType) &&
					strings.TrimSpace(selectedCredential.RefreshToken) != "" &&
					!refreshedCredentialAttempts[credentialID] {
					refreshedCredentialAttempts[credentialID] = true
					refreshed, refreshErr := p.refreshCredential(endpoint, selectedCredential)
					if refreshErr == nil {
						logger.Info("[%s] Credential refreshed after %d, retrying with updated token (id=%d)", endpoint.Name, resp.StatusCode, credentialID)
						p.markRequestInactive(endpoint.Name)
						endpointAttempts = 0
						if refreshed != nil && refreshed.ID > 0 {
							refreshedCredentialAttempts[refreshed.ID] = true
						}
						continue
					}
					logger.Warn("[%s] Credential refresh failed after %d (id=%d): %v", endpoint.Name, resp.StatusCode, credentialID, refreshErr)
				}
				p.markCredentialFailure(credentialID, resp.StatusCode, errMsg)
				p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				endpointAttempts = 0
				logger.Warn("[%s] Credential auth failed (%d), retrying with next token", endpoint.Name, resp.StatusCode)
				continue
			}
		}

		p.markRequestInactive(endpoint.Name)
		// Log non-200 responses for debugging
		if resp.StatusCode != http.StatusOK {
			errMsg := string(respBody)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
			if resp.StatusCode == http.StatusBadRequest &&
				strings.Contains(errMsg, "api.responses.write") &&
				strings.Contains(transformerName, "openai2") {
				logger.Warn("[%s] Upstream rejected /v1/responses scope (api.responses.write). Try transformer=openai (chat/completions) for this token.", endpoint.Name)
			}
			if skipCredentialPenalty {
				p.markCredentialFailure(credentialID, 0, errMsg)
				p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			} else {
				p.markCredentialFailure(credentialID, resp.StatusCode, errMsg)
				p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			}
			logger.Warn("[%s] Response %d: %s", endpoint.Name, resp.StatusCode, errMsg)
			logger.DebugLog("[%s] Response %d: %s", endpoint.Name, resp.StatusCode, errMsg)
		}
		// Remove Content-Encoding header since we've decompressed
		for key, values := range resp.Header {
			if key == "Content-Encoding" || key == "Content-Length" {
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	http.Error(w, "All endpoints failed", http.StatusServiceUnavailable)
}

func (p *Proxy) selectCredential(endpointName string) (*storage.EndpointCredential, error) {
	if p.storage == nil {
		return nil, nil
	}
	return p.storage.GetUsableEndpointCredential(endpointName, time.Now().UTC())
}

func (p *Proxy) markCredentialSuccess(credentialID int64) {
	if credentialID <= 0 || p.storage == nil {
		return
	}
	if err := p.storage.MarkCredentialSuccess(credentialID, time.Now().UTC()); err != nil {
		logger.Warn("Failed to mark credential success (id=%d): %v", credentialID, err)
	}
}

func (p *Proxy) recordCredentialUsage(credentialID int64, endpointName string, requests, errors, inputTokens, outputTokens int) {
	if credentialID <= 0 || p.storage == nil {
		return
	}
	if err := p.storage.UpsertCredentialUsage(credentialID, endpointName, requests, errors, inputTokens, outputTokens, time.Now().UTC()); err != nil {
		logger.Warn("Failed to record credential usage (id=%d): %v", credentialID, err)
	}
}

func (p *Proxy) markCredentialFailure(credentialID int64, statusCode int, errMsg string) {
	if credentialID <= 0 || p.storage == nil {
		return
	}
	if err := p.storage.MarkCredentialFailure(credentialID, statusCode, errMsg, time.Now().UTC()); err != nil {
		logger.Warn("Failed to mark credential failure (id=%d): %v", credentialID, err)
	}
}

func (p *Proxy) computeMaxRetries(endpoints []config.Endpoint) int {
	baseRetries := len(endpoints) * 2
	if p.storage == nil || len(endpoints) == 0 {
		return baseRetries
	}

	extraRetries := 0
	for _, endpoint := range endpoints {
		if !config.IsTokenPoolAuthMode(endpoint.AuthMode) {
			continue
		}

		stats, err := p.storage.GetTokenPoolStats(endpoint.Name)
		if err != nil {
			logger.Warn("[%s] Failed to load token pool stats: %v", endpoint.Name, err)
			continue
		}

		usable := stats.Active + stats.Expiring + stats.NeedRefresh
		if usable > 1 {
			extraRetries += usable - 1
		}
	}

	maxRetries := baseRetries + extraRetries
	if maxRetries < baseRetries {
		return baseRetries
	}
	return maxRetries
}

func shouldAggregateCodexStreaming(endpoint config.Endpoint, transformerName string) bool {
	if !strings.Contains(transformerName, "openai2") {
		return false
	}
	url := strings.ToLower(strings.TrimSpace(endpoint.APIUrl))
	return strings.Contains(url, "chatgpt.com/backend-api/codex")
}

// shouldHandleAsStreamingResponse determines if an upstream 200 response should be
// processed as SSE. Some Codex upstreams intermittently omit Content-Type even when
// stream=true and body is SSE.
func shouldHandleAsStreamingResponse(contentType string, clientRequestedStream bool, endpoint config.Endpoint, transformerName string) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream") {
		return true
	}
	if !clientRequestedStream {
		return false
	}
	// Codex /responses may return SSE with an empty content-type header.
	if shouldAggregateCodexStreaming(endpoint, transformerName) {
		return true
	}
	return false
}

func shouldTreatCredentialAuthFailure(statusCode int, body string) bool {
	if statusCode == http.StatusUnauthorized {
		return true
	}
	if statusCode != http.StatusForbidden {
		return false
	}

	lower := strings.ToLower(strings.TrimSpace(body))
	if strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.Contains(lower, "<head>") ||
		strings.Contains(lower, "<body") {
		return false
	}
	return true
}

func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "eof") {
		return true
	}
	if strings.Contains(message, "timeout awaiting response headers") {
		return true
	}
	if strings.Contains(message, "i/o timeout") {
		return true
	}
	if strings.Contains(message, "connection reset by peer") {
		return true
	}
	return false
}

// __validateAPIKey 验证 API Key 并检查端点权限（内部方法）
func (p *Proxy) __validateAPIKey(r *http.Request, w http.ResponseWriter, bodyBytes []byte) bool {
	// 从 Header 或 Query 参数获取 API Key
	// 支持以下格式（密钥值直接使用，不做任何处理）：
	// 1. X-API-Key: <密钥>
	// 2. Authorization: Bearer <密钥>
	// 3. ?api_key=<密钥>
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		// 尝试从 Authorization header 获取
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			// 支持 "Bearer token" 格式
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
				apiKey = parts[1]
			}
		}
	}
	if apiKey == "" {
		apiKey = r.URL.Query().Get("api_key")
	}

	logger.DebugLog("[API Key Auth] API Key provided: %s", maskAPIKeyForLog(apiKey))

	if apiKey == "" {
		logger.Warn("[API Key Auth] No API Key provided")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"API Key is required"}}`))
		return false
	}

	// 验证 API Key
	keyWithPermissions, err := p.storage.GetAPIKeyByKeyValue(apiKey)
	if err != nil {
		logger.Error("[API Key Auth] Failed to validate API key: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"type":"internal_error","message":"Failed to validate API key"}}`))
		return false
	}

	if keyWithPermissions == nil {
		logger.Warn("[API Key Auth] Invalid API Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"Invalid API Key"}}`))
		return false
	}

	logger.DebugLog("[API Key Auth] Key found: name=%s, enabled=%v, endpoints=%v",
		keyWithPermissions.Name, keyWithPermissions.Enabled, keyWithPermissions.EndpointNames)

	// 检查 API Key 是否启用
	if !keyWithPermissions.Enabled {
		logger.Warn("[API Key Auth] API Key is disabled: %s", keyWithPermissions.Name)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"API Key is disabled"}}`))
		return false
	}

	// 检查 API Key 是否过期
	if keyWithPermissions.ExpiresAt != nil && time.Now().After(*keyWithPermissions.ExpiresAt) {
		logger.Warn("[API Key Auth] API Key has expired: %s", keyWithPermissions.Name)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"API Key has expired"}}`))
		return false
	}

	// 获取请求的目标端点
	specifiedEndpoint, modelOverride, resolveErr := p.resolver.ResolveEndpoint(r, bodyBytes)

	logger.DebugLog("[API Key Auth] Endpoint resolution: specifiedEndpoint=%v, modelOverride=%v, resolveErr=%v",
		specifiedEndpoint, modelOverride, resolveErr)

	// 打印 key 允许的端点列表
	logger.DebugLog("[API Key Auth] Key '%s' permitted endpoints: %v", keyWithPermissions.Name, keyWithPermissions.EndpointNames)

	// 用户是否明确指定了端点
	userSpecifiedEndpoint := specifiedEndpoint != nil && resolveErr == nil

	if userSpecifiedEndpoint {
		// 用户指定了端点，检查是否有权限
		targetEndpointName := specifiedEndpoint.Name
		hasPermission := false
		for _, allowedName := range keyWithPermissions.EndpointNames {
			if allowedName == targetEndpointName {
				hasPermission = true
				break
			}
		}

		logger.DebugLog("[API Key Auth] User specified endpoint '%s', hasPermission=%v", targetEndpointName, hasPermission)

		if !hasPermission {
			logger.Warn("[API Key Auth] Access denied to endpoint '%s' for key '%s' (not in permitted list: %v)",
				targetEndpointName, keyWithPermissions.Name, keyWithPermissions.EndpointNames)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"type":"authentication_error","message":"Access denied to endpoint '` + targetEndpointName + `'"}}`))
			return false
		}

		logger.Info("[API Key Auth] Access granted: key=%s, endpoint=%s (user specified)", keyWithPermissions.Name, targetEndpointName)
	} else {
		// 用户没有指定端点，从 key 的允许端点中自动选择
		logger.DebugLog("[API Key Auth] No endpoint specified by user, auto-selecting from permitted endpoints")

		// 解析用户请求的模型
		var requestPayload struct {
			Model string `json:"model"`
		}
		var requestedModel string
		if err := json.Unmarshal(bodyBytes, &requestPayload); err == nil {
			requestedModel = strings.TrimSpace(requestPayload.Model)
			logger.DebugLog("[API Key Auth] User requested model: %s", requestedModel)
		}

		allEndpoints := p.getEnabledEndpoints()
		var selectedEndpoint *config.Endpoint
		var selectedEndpointName string

		if requestedModel != "" {
			// 如果用户指定了模型，优先选择支持该模型的端点
			logger.DebugLog("[API Key Auth] Looking for endpoint that supports model '%s'", requestedModel)
			for _, allowedName := range keyWithPermissions.EndpointNames {
				for _, ep := range allEndpoints {
					if ep.Name == allowedName && ep.Model == requestedModel {
						selectedEndpoint = &ep
						selectedEndpointName = allowedName
						logger.DebugLog("[API Key Auth] Found endpoint '%s' with matching model '%s'", allowedName, requestedModel)
						break
					}
				}
				if selectedEndpoint != nil {
					break
				}
			}
		}

		// 如果没有找到匹配模型的端点，按顺序选择第一个有权限的端点
		if selectedEndpoint == nil {
			logger.DebugLog("[API Key Auth] No exact model match found, selecting first permitted endpoint")
			for _, allowedName := range keyWithPermissions.EndpointNames {
				logger.DebugLog("[API Key Auth] Checking permitted endpoint: %s", allowedName)
				for _, ep := range allEndpoints {
					if ep.Name == allowedName {
						selectedEndpoint = &ep
						selectedEndpointName = allowedName
						logger.DebugLog("[API Key Auth] Found enabled endpoint: %s (model: %s)", allowedName, ep.Model)
						break
					}
				}
				if selectedEndpoint != nil {
					break
				}
			}
		}

		if selectedEndpoint == nil {
			logger.Warn("[API Key Auth] No enabled endpoints found for key '%s' (permitted: %v)",
				keyWithPermissions.Name, keyWithPermissions.EndpointNames)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"type":"authentication_error","message":"No accessible endpoints available"}}`))
			return false
		}

		logger.Info("[API Key Auth] Auto-selected endpoint '%s' (model: %s) for key '%s'",
			selectedEndpointName, selectedEndpoint.Model, keyWithPermissions.Name)

		// 将选中的端点存储到 context 中，供 handleProxy 使用
		ctx := context.WithValue(r.Context(), "selectedEndpoint", selectedEndpoint)
		*r = *r.WithContext(ctx)
	}

	// 更新最后使用时间
	if err := p.storage.UpdateAPIKeyLastUsed(apiKey); err != nil {
		logger.Warn("[API Key Auth] Failed to update API key last used time: %v", err)
	}

	return true
}

// maskAPIKeyForLog masks API key for logging (show first 8 and last 4 chars)
func maskAPIKeyForLog(key string) string {
	if len(key) <= 12 {
		return "***"
	}
	return key[:8] + "***" + key[len(key)-4:]
}
