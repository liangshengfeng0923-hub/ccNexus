package service

import (
	"testing"
)

func TestDetectAPIVersionInURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "智谱 API v4",
			url:      "https://open.bigmodel.cn/api/paas/v4",
			expected: "v4",
		},
		{
			name:     "OpenAI with v1",
			url:      "https://api.openai.com/v1",
			expected: "v1",
		},
		{
			name:     "无版本号的 URL",
			url:      "https://api.example.com",
			expected: "",
		},
		{
			name:     "v2 版本",
			url:      "https://api.example.com/api/v2",
			expected: "v2",
		},
		{
			name:     "Codex backend",
			url:      "https://chatgpt.com/backend-api/codex",
			expected: "",
		},
		{
			name:     "多路径段带版本",
			url:      "https://api.example.com/api/paas/v4/endpoints",
			expected: "v4",
		},
		{
			name:     "带尾部斜杠",
			url:      "https://open.bigmodel.cn/api/paas/v4/",
			expected: "v4",
		},
		{
			name:     "多位数版本",
			url:      "https://api.example.com/v10",
			expected: "v10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectAPIVersionInURL(tt.url)
			if result != tt.expected {
				t.Errorf("detectAPIVersionInURL(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestBuildOpenAIModelsURL(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		isCodexBackend bool
		expected       string
	}{
		{
			name:           "智谱 API v4",
			url:            "https://open.bigmodel.cn/api/paas/v4",
			isCodexBackend: false,
			expected:       "https://open.bigmodel.cn/api/paas/v4/models",
		},
		{
			name:           "无版本号 OpenAI",
			url:            "https://api.openai.com",
			isCodexBackend: false,
			expected:       "https://api.openai.com/v1/models",
		},
		{
			name:           "带 v1 的 URL",
			url:            "https://api.example.com/v1",
			isCodexBackend: false,
			expected:       "https://api.example.com/v1/models",
		},
		{
			name:           "Codex backend",
			url:            "https://chatgpt.com/backend-api/codex",
			isCodexBackend: true,
			expected:       "https://chatgpt.com/backend-api/codex/models",
		},
		{
			name:           "带尾部斜杠",
			url:            "https://open.bigmodel.cn/api/paas/v4/",
			isCodexBackend: false,
			expected:       "https://open.bigmodel.cn/api/paas/v4/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildOpenAIModelsURL(tt.url, tt.isCodexBackend)
			if result != tt.expected {
				t.Errorf("buildOpenAIModelsURL(%q, %v) = %q, want %q", tt.url, tt.isCodexBackend, result, tt.expected)
			}
		})
	}
}
