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
			"base_url":                      "http://127.0.0.1:11434/v1",
			"api_key":                       "",
			"model":                         "nomic-embed-text",
			"timeout_seconds":               30,
			"semantic_similarity_threshold": 0.2,
			"semantic_candidate_batch_size": 128,
			"semantic_candidate_max_count":  512,
			"semantic_hit_fetch_limit":      32,
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
	if got.SemanticSimilarityThreshold != 0.2 {
		t.Fatalf("SemanticSimilarityThreshold = %v, want 0.2", got.SemanticSimilarityThreshold)
	}
	if got.SemanticCandidateBatchSize != 128 {
		t.Fatalf("SemanticCandidateBatchSize = %d, want 128", got.SemanticCandidateBatchSize)
	}
	if got.SemanticCandidateMaxCount != 512 {
		t.Fatalf("SemanticCandidateMaxCount = %d, want 512", got.SemanticCandidateMaxCount)
	}
	if got.SemanticHitFetchLimit != 32 {
		t.Fatalf("SemanticHitFetchLimit = %d, want 32", got.SemanticHitFetchLimit)
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

// TestResolveEmbeddingConfigUsesSemanticDefaults 验证未显式配置语义检索参数时会自动回落到统一默认值。
func TestResolveEmbeddingConfigUsesSemanticDefaults(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"embedding": map[string]any{
			"base_url": "http://127.0.0.1:11434/v1",
			"model":    "nomic-embed-text",
		},
	}
	got := resolveEmbeddingConfig(payload)
	if got == nil {
		t.Fatal("resolveEmbeddingConfig() 返回 nil, want 非空配置")
	}
	if got.SemanticSimilarityThreshold != defaultSemanticSimilarityThreshold {
		t.Fatalf("SemanticSimilarityThreshold = %v, want %v", got.SemanticSimilarityThreshold, defaultSemanticSimilarityThreshold)
	}
	if got.SemanticCandidateBatchSize != defaultSemanticCandidateBatchSize {
		t.Fatalf("SemanticCandidateBatchSize = %d, want %d", got.SemanticCandidateBatchSize, defaultSemanticCandidateBatchSize)
	}
	if got.SemanticCandidateMaxCount != defaultSemanticCandidateMaxCount {
		t.Fatalf("SemanticCandidateMaxCount = %d, want %d", got.SemanticCandidateMaxCount, defaultSemanticCandidateMaxCount)
	}
	if got.SemanticHitFetchLimit != defaultSemanticHitFetchLimit {
		t.Fatalf("SemanticHitFetchLimit = %d, want %d", got.SemanticHitFetchLimit, defaultSemanticHitFetchLimit)
	}
}

// TestResolveSearchConfig 验证搜索结果上限可从配置读取，避免不同服务实例返回条数不一致。
func TestResolveSearchConfig(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"search": map[string]any{
			"low_confidence_error_hit_limit":   3,
			"low_confidence_summary_hit_limit": 5,
		},
	}
	got := resolveSearchConfig(payload)
	if got == nil {
		t.Fatal("resolveSearchConfig() 返回 nil, want 非空配置")
	}
	if got.LowConfidenceErrorHitLimit != 3 {
		t.Fatalf("LowConfidenceErrorHitLimit = %d, want 3", got.LowConfidenceErrorHitLimit)
	}
	if got.LowConfidenceSummaryHitLimit != 5 {
		t.Fatalf("LowConfidenceSummaryHitLimit = %d, want 5", got.LowConfidenceSummaryHitLimit)
	}
}

// TestResolveSearchConfigUsesDefaults 验证缺省场景会回退到统一默认上限，避免旧配置文件升级后行为漂移。
func TestResolveSearchConfigUsesDefaults(t *testing.T) {
	t.Helper()
	got := resolveSearchConfig(map[string]any{})
	if got == nil {
		t.Fatal("resolveSearchConfig() 返回 nil, want 非空配置")
	}
	if got.LowConfidenceErrorHitLimit != defaultSearchErrorHitLimit {
		t.Fatalf("LowConfidenceErrorHitLimit = %d, want %d", got.LowConfidenceErrorHitLimit, defaultSearchErrorHitLimit)
	}
	if got.LowConfidenceSummaryHitLimit != defaultSearchSummaryHitLimit {
		t.Fatalf("LowConfidenceSummaryHitLimit = %d, want %d", got.LowConfidenceSummaryHitLimit, defaultSearchSummaryHitLimit)
	}
}
