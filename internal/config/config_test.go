package config

import "testing"

// TestResolveServerListenAddr 验证监听地址可从配置读取，避免服务端继续回退到固定端口。
func TestResolveServerListenAddr(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"server": map[string]any{
			"listen_addr": ":19090",
		},
	}
	if got := resolveServerListenAddr(payload); got != ":19090" {
		t.Fatalf("resolveServerListenAddr() = %q, want %q", got, ":19090")
	}

	if got := resolveServerListenAddr(map[string]any{}); got != defaultServerListenAddr {
		t.Fatalf("默认监听地址异常: got=%q want=%q", got, defaultServerListenAddr)
	}
}

// TestResolveEmbeddingConfigWithoutAPIKey 验证本地免鉴权嵌入服务不会因空 api_key 被误判为未配置。
func TestResolveEmbeddingConfigWithoutAPIKey(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"embedding": map[string]any{
			"base_url":        "http://127.0.0.1:11434/v1",
			"api_key":         "",
			"model":           "nomic-embed-text",
			"timeout_seconds": 30,
		},
	}
	got := resolveEmbeddingConfig(payload)
	if got == nil {
		t.Fatal("resolveEmbeddingConfig() 返回 nil, want 非空配置")
	}
	if got.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("BaseURL = %q, want %q", got.BaseURL, "http://127.0.0.1:11434/v1")
	}
	if got.Model != "nomic-embed-text" {
		t.Fatalf("Model = %q, want %q", got.Model, "nomic-embed-text")
	}
	if got.APIKey != "" {
		t.Fatalf("APIKey = %q, want 空字符串", got.APIKey)
	}
	if got.TimeoutSeconds != 30 {
		t.Fatalf("TimeoutSeconds = %v, want 30", got.TimeoutSeconds)
	}
}

// TestResolveEmbeddingConfigRequiresModel 验证缺少模型名时仍保持关闭，避免错误把半配置状态当成可用服务。
func TestResolveEmbeddingConfigRequiresModel(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"embedding": map[string]any{
			"base_url": "http://127.0.0.1:11434/v1",
		},
	}
	if got := resolveEmbeddingConfig(payload); got != nil {
		t.Fatalf("resolveEmbeddingConfig() = %#v, want nil", got)
	}
}
