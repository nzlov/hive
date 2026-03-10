package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if got.SemanticWindowMode != defaultSemanticWindowMode {
		t.Fatalf("SemanticWindowMode = %q, want %q", got.SemanticWindowMode, defaultSemanticWindowMode)
	}
	if got.SemanticWindowBaseMaxCount != defaultSemanticCandidateMaxCount {
		t.Fatalf("SemanticWindowBaseMaxCount = %d, want %d", got.SemanticWindowBaseMaxCount, defaultSemanticCandidateMaxCount)
	}
	if got.DecayEnabled != defaultDecayEnabled {
		t.Fatalf("DecayEnabled = %v, want %v", got.DecayEnabled, defaultDecayEnabled)
	}
	if got.DecaySummaryHalfLifeDays != defaultDecaySummaryHalfLifeDays {
		t.Fatalf("DecaySummaryHalfLifeDays = %v, want %v", got.DecaySummaryHalfLifeDays, defaultDecaySummaryHalfLifeDays)
	}
	if got.DecayErrorHalfLifeDays != defaultDecayErrorHalfLifeDays {
		t.Fatalf("DecayErrorHalfLifeDays = %v, want %v", got.DecayErrorHalfLifeDays, defaultDecayErrorHalfLifeDays)
	}
}

// TestResolveEmbeddingConfigParsesSemanticWindowAndDecay 验证语义窗口与时间衰减参数可由配置覆盖，避免后续策略仍写死在代码里。
func TestResolveEmbeddingConfigParsesSemanticWindowAndDecay(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"embedding": map[string]any{
			"base_url": "http://127.0.0.1:11434/v1",
			"model":    "nomic-embed-text",
			"semantic_window": map[string]any{
				"mode":                  "dynamic",
				"base_max_count":        2048,
				"dynamic_min_count":     300,
				"dynamic_max_count":     12000,
				"dynamic_ratio":         0.35,
				"reference_corpus_size": 20000,
			},
			"decay": map[string]any{
				"enabled":         false,
				"age_weight":      0.3,
				"semantic_weight": 0.7,
				"half_life_days": map[string]any{
					"summary": 15,
					"error":   120,
				},
			},
		},
	}
	got := resolveEmbeddingConfig(payload)
	if got == nil {
		t.Fatal("resolveEmbeddingConfig() 返回 nil, want 非空配置")
	}
	if got.SemanticWindowMode != "dynamic" {
		t.Fatalf("SemanticWindowMode = %q, want dynamic", got.SemanticWindowMode)
	}
	if got.SemanticWindowBaseMaxCount != 2048 {
		t.Fatalf("SemanticWindowBaseMaxCount = %d, want 2048", got.SemanticWindowBaseMaxCount)
	}
	if got.SemanticWindowDynamicMin != 300 {
		t.Fatalf("SemanticWindowDynamicMin = %d, want 300", got.SemanticWindowDynamicMin)
	}
	if got.SemanticWindowDynamicMax != 12000 {
		t.Fatalf("SemanticWindowDynamicMax = %d, want 12000", got.SemanticWindowDynamicMax)
	}
	if got.SemanticWindowDynamicRatio != 0.35 {
		t.Fatalf("SemanticWindowDynamicRatio = %v, want 0.35", got.SemanticWindowDynamicRatio)
	}
	if got.SemanticWindowReferenceSize != 20000 {
		t.Fatalf("SemanticWindowReferenceSize = %d, want 20000", got.SemanticWindowReferenceSize)
	}
	if got.DecayEnabled {
		t.Fatalf("DecayEnabled = %v, want false", got.DecayEnabled)
	}
	if got.DecayAgeWeight != 0.3 {
		t.Fatalf("DecayAgeWeight = %v, want 0.3", got.DecayAgeWeight)
	}
	if got.DecaySemanticWeight != 0.7 {
		t.Fatalf("DecaySemanticWeight = %v, want 0.7", got.DecaySemanticWeight)
	}
	if got.DecaySummaryHalfLifeDays != 15 {
		t.Fatalf("DecaySummaryHalfLifeDays = %v, want 15", got.DecaySummaryHalfLifeDays)
	}
	if got.DecayErrorHalfLifeDays != 120 {
		t.Fatalf("DecayErrorHalfLifeDays = %v, want 120", got.DecayErrorHalfLifeDays)
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
	if got.KeywordMode != defaultKeywordMode {
		t.Fatalf("KeywordMode = %q, want %q", got.KeywordMode, defaultKeywordMode)
	}
	if got.KeywordBM25K1 != defaultKeywordBM25K1 {
		t.Fatalf("KeywordBM25K1 = %v, want %v", got.KeywordBM25K1, defaultKeywordBM25K1)
	}
	if got.KeywordBM25B != defaultKeywordBM25B {
		t.Fatalf("KeywordBM25B = %v, want %v", got.KeywordBM25B, defaultKeywordBM25B)
	}
	if got.FusionFormula != defaultFusionFormula {
		t.Fatalf("FusionFormula = %q, want %q", got.FusionFormula, defaultFusionFormula)
	}
	if got.CacheMaxEntries != defaultCacheMaxEntries {
		t.Fatalf("CacheMaxEntries = %d, want %d", got.CacheMaxEntries, defaultCacheMaxEntries)
	}
	if got.CacheStatsRefreshInterval != defaultCacheStatsRefreshInterval {
		t.Fatalf("CacheStatsRefreshInterval = %d, want %d", got.CacheStatsRefreshInterval, defaultCacheStatsRefreshInterval)
	}
}

// TestResolveSearchConfigParsesKeywordFusionCache 验证关键字、融合和缓存参数可由配置控制，避免后续实现出现隐式写死。
func TestResolveSearchConfigParsesKeywordFusionCache(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"search": map[string]any{
			"keyword": map[string]any{
				"mode":    "bm25",
				"backend": "postgres",
				"fields":  []string{"title", "content"},
				"field_weights": map[string]float64{
					"title":   3,
					"content": 1,
				},
				"synonyms": map[string]any{
					"enabled": true,
					"groups": [][]string{
						{"error", "故障"},
					},
				},
			},
			"fusion": map[string]any{
				"enabled":            true,
				"formula":            "weighted_sum",
				"keyword_weight":     0.6,
				"semantic_weight":    0.3,
				"recency_weight":     0.1,
				"min_semantic_score": 0.2,
			},
			"cache": map[string]any{
				"enabled":                        true,
				"query_embedding_ttl_seconds":    300,
				"semantic_hits_ttl_seconds":      60,
				"max_entries":                    2000,
				"stats_refresh_interval_seconds": 5,
			},
		},
	}
	got := resolveSearchConfig(payload)
	if got == nil {
		t.Fatal("resolveSearchConfig() 返回 nil, want 非空配置")
	}
	if got.KeywordMode != "bm25" {
		t.Fatalf("KeywordMode = %q, want bm25", got.KeywordMode)
	}
	if got.KeywordBackend != "postgres" {
		t.Fatalf("KeywordBackend = %q, want postgres", got.KeywordBackend)
	}
	if len(got.KeywordFields) != 2 || got.KeywordFields[0] != "title" {
		t.Fatalf("KeywordFields = %#v, want [title content]", got.KeywordFields)
	}
	if got.KeywordFieldWeights["title"] != 3 {
		t.Fatalf("KeywordFieldWeights[title] = %v, want 3", got.KeywordFieldWeights["title"])
	}
	if got.KeywordBM25K1 != defaultKeywordBM25K1 {
		t.Fatalf("KeywordBM25K1 = %v, want %v", got.KeywordBM25K1, defaultKeywordBM25K1)
	}
	if got.KeywordBM25B != defaultKeywordBM25B {
		t.Fatalf("KeywordBM25B = %v, want %v", got.KeywordBM25B, defaultKeywordBM25B)
	}
	if !got.KeywordSynonymsEnabled {
		t.Fatalf("KeywordSynonymsEnabled = %v, want true", got.KeywordSynonymsEnabled)
	}
	if len(got.KeywordSynonymGroups) != 1 || len(got.KeywordSynonymGroups[0]) != 2 {
		t.Fatalf("KeywordSynonymGroups = %#v, want [[error 故障]]", got.KeywordSynonymGroups)
	}
	if !got.FusionEnabled {
		t.Fatalf("FusionEnabled = %v, want true", got.FusionEnabled)
	}
	if got.FusionKeywordWeight != 0.6 {
		t.Fatalf("FusionKeywordWeight = %v, want 0.6", got.FusionKeywordWeight)
	}
	if got.FusionSemanticWeight != 0.3 {
		t.Fatalf("FusionSemanticWeight = %v, want 0.3", got.FusionSemanticWeight)
	}
	if got.FusionRecencyWeight != 0.1 {
		t.Fatalf("FusionRecencyWeight = %v, want 0.1", got.FusionRecencyWeight)
	}
	if got.FusionMinSemanticScore != 0.2 {
		t.Fatalf("FusionMinSemanticScore = %v, want 0.2", got.FusionMinSemanticScore)
	}
	if !got.CacheEnabled {
		t.Fatalf("CacheEnabled = %v, want true", got.CacheEnabled)
	}
	if got.CacheQueryEmbeddingTTL != 300 {
		t.Fatalf("CacheQueryEmbeddingTTL = %d, want 300", got.CacheQueryEmbeddingTTL)
	}
	if got.CacheSemanticHitsTTL != 60 {
		t.Fatalf("CacheSemanticHitsTTL = %d, want 60", got.CacheSemanticHitsTTL)
	}
	if got.CacheMaxEntries != 2000 {
		t.Fatalf("CacheMaxEntries = %d, want 2000", got.CacheMaxEntries)
	}
	if got.CacheStatsRefreshInterval != 5 {
		t.Fatalf("CacheStatsRefreshInterval = %d, want 5", got.CacheStatsRefreshInterval)
	}
}

// TestLoadPayloadSupportsJSONCAndAutoFillsMissingKeys 验证 JSONC 配置可解析且会自动补齐缺失键，避免版本升级后仍需手工补字段。
func TestLoadPayloadSupportsJSONCAndAutoFillsMissingKeys(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	raw := `{
  // 只保留基础 server 配置，其他字段让加载流程自动补齐
  "server": {
    "base_url": "http://127.0.0.1:19090"
  }
}`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	payload, err := loadPayload(configPath)
	if err != nil {
		t.Fatalf("loadPayload 返回错误: %v", err)
	}
	if _, ok := payload["search"]; !ok {
		t.Fatalf("应自动补齐 search 配置: %#v", payload)
	}
	embedding, ok := payload["embedding"].(map[string]any)
	if !ok {
		t.Fatalf("应自动补齐 embedding 配置: %#v", payload)
	}
	if _, ok := embedding["semantic_window"]; !ok {
		t.Fatalf("应自动补齐 semantic_window 配置: %#v", embedding)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取补齐后的配置失败: %v", err)
	}
	if !strings.Contains(string(updated), "\"base_url\": \"http://127.0.0.1:19090\", //") {
		t.Fatalf("补齐后配置应把注释放在配置项后面: %s", string(updated))
	}
}

// TestLoadPayloadKeepsExistingValuesWhenFillingDefaults 验证自动补齐仅填缺失项，避免覆盖用户现有配置。
func TestLoadPayloadKeepsExistingValuesWhenFillingDefaults(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	raw := `{
  "search": {
    "low_confidence_error_hit_limit": 3
  },
  "embedding": {
    "base_url": "http://127.0.0.1:11434/v1",
    "model": "nomic-embed-text"
  }
}`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	payload, err := loadPayload(configPath)
	if err != nil {
		t.Fatalf("loadPayload 返回错误: %v", err)
	}
	search, ok := payload["search"].(map[string]any)
	if !ok {
		t.Fatalf("search 配置类型异常: %#v", payload["search"])
	}
	if got := pickInt(search, searchLowConfidenceErrorHitLimitKeys, 0); got != 3 {
		t.Fatalf("已有错误命中上限不应被覆盖: got=%d want=3", got)
	}
	if got := pickInt(search, searchLowConfidenceSummaryHitLimitKeys, 0); got != defaultSearchSummaryHitLimit {
		t.Fatalf("缺失的总结命中上限应补默认值: got=%d want=%d", got, defaultSearchSummaryHitLimit)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取补齐后的配置失败: %v", err)
	}
	if !strings.Contains(string(updated), "\"low_confidence_error_hit_limit\": 3") {
		t.Fatalf("补齐后应保留已有配置值: %s", string(updated))
	}
}

// TestStripJSONCCommentsKeepsURLLiterals 验证注释清理不会破坏 URL 字符串，避免 base_url 中的 // 被误删。
func TestStripJSONCCommentsKeepsURLLiterals(t *testing.T) {
	t.Helper()
	raw := "{\n  \"url\": \"http://127.0.0.1:8080\", // 注释\n  /* 块注释 */\n  \"ok\": true\n}\n"
	cleaned := stripJSONCComments(raw)
	if !strings.Contains(cleaned, "http://127.0.0.1:8080") {
		t.Fatalf("URL 字符串不应被注释清理破坏: %s", cleaned)
	}
	if strings.Contains(cleaned, "注释") {
		t.Fatalf("注释内容应被清理: %s", cleaned)
	}
}
