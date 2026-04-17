package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/storage"
)

// handleAPIKeys handles GET (list) and POST (create) for API keys
func (h *Handler) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listAPIKeys(w, r)
	case http.MethodPost:
		h.createAPIKey(w, r)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIKeyByID handles GET, PUT, DELETE for specific API key
func (h *Handler) handleAPIKeyByID(w http.ResponseWriter, r *http.Request) {
	// Extract key ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/apikeys/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		WriteError(w, http.StatusBadRequest, "API key ID required")
		return
	}

	// Handle /regenerate sub-path
	if len(parts) > 1 && parts[1] == "regenerate" {
		h.regenerateAPIKey(w, r, parts[0])
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getAPIKey(w, r, id)
	case http.MethodPut:
		h.updateAPIKey(w, r, id)
	case http.MethodDelete:
		h.deleteAPIKey(w, r, id)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIKeysConfig handles GET and PUT for API keys config
func (h *Handler) handleAPIKeysConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getAPIKeysConfig(w, r)
	case http.MethodPut:
		h.updateAPIKeysConfig(w, r)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// listAPIKeys returns all API keys
func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	apiKeys, err := h.storage.GetAPIKeys()
	if err != nil {
		logger.Error("Failed to get API keys: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get API keys")
		return
	}

	WriteSuccess(w, apiKeys)
}

// getAPIKey returns a specific API key
func (h *Handler) getAPIKey(w http.ResponseWriter, r *http.Request, id int64) {
	apiKey, err := h.storage.GetAPIKeyByID(id)
	if err != nil {
		logger.Error("Failed to get API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get API key")
		return
	}

	if apiKey == nil {
		WriteError(w, http.StatusNotFound, "API key not found")
		return
	}

	WriteSuccess(w, apiKey)
}

// createAPIKeyRequest represents the request to create an API key
type createAPIKeyRequest struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	ExpiresAt     string   `json:"expiresAt"`     // ISO 8601 format
	EndpointNames []string `json:"endpointNames"`
}

// createAPIKey creates a new API key
func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "Name is required")
		return
	}

	if len(req.EndpointNames) == 0 {
		WriteError(w, http.StatusBadRequest, "At least one endpoint must be specified")
		return
	}

	// 验证端点是否存在
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to validate endpoints")
		return
	}

	endpointMap := make(map[string]bool)
	for _, ep := range endpoints {
		endpointMap[ep.Name] = true
	}

	for _, name := range req.EndpointNames {
		if !endpointMap[name] {
			WriteError(w, http.StatusBadRequest, "Endpoint '"+name+"' does not exist")
			return
		}
	}

	// 创建 API Key
	keyValue := "starnet-" + uuid.New().String()

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		parsedTime, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid expires_at format")
			return
		}
		expiresAt = &parsedTime
	}

	apiKey := &storage.APIKey{
		KeyValue:  keyValue,
		Name:      req.Name,
		Enabled:   req.Enabled,
		ExpiresAt: expiresAt,
	}

	if err := h.storage.SaveAPIKey(apiKey, req.EndpointNames); err != nil {
		logger.Error("Failed to save API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to save API key")
		return
	}

	// 获取完整的 API Key 信息返回
	result, err := h.storage.GetAPIKeyByKeyValue(keyValue)
	if err != nil {
		logger.Error("Failed to retrieve created API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to retrieve created API key")
		return
	}

	WriteSuccess(w, result)
}

// updateAPIKeyRequest represents the request to update an API key
type updateAPIKeyRequest struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	ExpiresAt     string   `json:"expiresAt"`
	EndpointNames []string `json:"endpointNames"`
}

// updateAPIKey updates an existing API key
func (h *Handler) updateAPIKey(w http.ResponseWriter, r *http.Request, id int64) {
	var req updateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// 获取现有 API Key
	existingKey, err := h.storage.GetAPIKeyByID(id)
	if err != nil {
		logger.Error("Failed to get API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get API key")
		return
	}

	if existingKey == nil {
		WriteError(w, http.StatusNotFound, "API key not found")
		return
	}

	// 验证端点是否存在
	if len(req.EndpointNames) > 0 {
		endpoints, err := h.storage.GetEndpoints()
		if err != nil {
			logger.Error("Failed to get endpoints: %v", err)
			WriteError(w, http.StatusInternalServerError, "Failed to validate endpoints")
			return
		}

		endpointMap := make(map[string]bool)
		for _, ep := range endpoints {
			endpointMap[ep.Name] = true
		}

		for _, name := range req.EndpointNames {
			if !endpointMap[name] {
				WriteError(w, http.StatusBadRequest, "Endpoint '"+name+"' does not exist")
				return
			}
		}
	}

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		parsedTime, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid expires_at format")
			return
		}
		expiresAt = &parsedTime
	}

	apiKey := &storage.APIKey{
		ID:        id,
		Name:      req.Name,
		Enabled:   req.Enabled,
		ExpiresAt: expiresAt,
		KeyValue:  existingKey.KeyValue,
	}

	if err := h.storage.UpdateAPIKey(apiKey, req.EndpointNames); err != nil {
		logger.Error("Failed to update API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to update API key")
		return
	}

	// 获取更新后的 API Key 信息返回
	result, err := h.storage.GetAPIKeyByID(id)
	if err != nil {
		logger.Error("Failed to retrieve updated API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to retrieve updated API key")
		return
	}

	WriteSuccess(w, result)
}

// deleteAPIKey deletes an API key
func (h *Handler) deleteAPIKey(w http.ResponseWriter, r *http.Request, id int64) {
	// 检查 API Key 是否存在
	existingKey, err := h.storage.GetAPIKeyByID(id)
	if err != nil {
		logger.Error("Failed to get API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get API key")
		return
	}

	if existingKey == nil {
		WriteError(w, http.StatusNotFound, "API key not found")
		return
	}

	if err := h.storage.DeleteAPIKey(id); err != nil {
		logger.Error("Failed to delete API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to delete API key")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"message": "API key deleted successfully",
	})
}

// regenerateAPIKey regenerates an API key (keeps permissions)
func (h *Handler) regenerateAPIKey(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	// 获取现有 API Key
	existingKey, err := h.storage.GetAPIKeyByID(id)
	if err != nil {
		logger.Error("Failed to get API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get API key")
		return
	}

	if existingKey == nil {
		WriteError(w, http.StatusNotFound, "API key not found")
		return
	}

	// 生成新的 Key 值
	newKeyValue := "starnet-" + uuid.New().String()

	apiKey := &storage.APIKey{
		ID:        id,
		Name:      existingKey.Name,
		Enabled:   existingKey.Enabled,
		ExpiresAt: existingKey.ExpiresAt,
		KeyValue:  newKeyValue,
	}

	if err := h.storage.UpdateAPIKey(apiKey, existingKey.EndpointNames); err != nil {
		logger.Error("Failed to regenerate API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to regenerate API key")
		return
	}

	// 获取更新后的 API Key 信息返回
	result, err := h.storage.GetAPIKeyByID(id)
	if err != nil {
		logger.Error("Failed to retrieve regenerated API key: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to retrieve regenerated API key")
		return
	}

	WriteSuccess(w, result)
}

// updateAPIKeysConfigRequest represents the request to update API keys config
type updateAPIKeysConfigRequest struct {
	Enabled bool `json:"enabled"`
}

// getAPIKeysConfig returns the API keys configuration
func (h *Handler) getAPIKeysConfig(w http.ResponseWriter, r *http.Request) {
	enabledStr, err := h.storage.GetConfig("api_key_auth_enabled")
	if err != nil {
		logger.Error("Failed to get API keys config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get API keys config")
		return
	}

	enabled := enabledStr == "true"

	WriteSuccess(w, map[string]interface{}{
		"enabled": enabled,
	})
}

// updateAPIKeysConfig updates the API keys configuration
func (h *Handler) updateAPIKeysConfig(w http.ResponseWriter, r *http.Request) {
	var req updateAPIKeysConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	value := "false"
	if req.Enabled {
		value = "true"
	}

	if err := h.storage.SetConfig("api_key_auth_enabled", value); err != nil {
		logger.Error("Failed to update API keys config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to update API keys config")
		return
	}

	// 更新运行时配置
	h.config.APIKeyAuthEnabled = req.Enabled

	WriteSuccess(w, map[string]interface{}{
		"enabled": req.Enabled,
		"message": "API key authentication configuration updated",
	})
}
