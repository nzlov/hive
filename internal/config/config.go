package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultServerBaseURL               = "http://127.0.0.1:8080"
	defaultServerListenAddr            = ":8080"
	defaultJWTSecret                   = "hive-change-me"
	defaultSearchErrorHitLimit         = 10
	defaultSearchSummaryHitLimit       = 10
	defaultSemanticSimilarityThreshold = 0.15
	defaultSemanticCandidateBatchSize  = 256
	defaultSemanticCandidateMaxCount   = 1024
	defaultSemanticHitFetchLimit       = 64
)

// EmbeddingConfig 统一描述嵌入配置，避免不同模块各自解释字段语义。
type EmbeddingConfig struct {
	BaseURL                     string
	APIKey                      string
	Model                       string
	TimeoutSeconds              float64
	SemanticSimilarityThreshold float64
	SemanticCandidateBatchSize  int
	SemanticCandidateMaxCount   int
	SemanticHitFetchLimit       int
}

// DatabaseConfig 统一描述数据库连接信息，兼容本地 SQLite 和远端 PostgreSQL 两种模式。
type DatabaseConfig struct {
	Driver string
	DSN    string
}

// SearchConfig 统一描述搜索结果裁剪策略，避免不同搜索入口返回条数不一致。
type SearchConfig struct {
	LowConfidenceErrorHitLimit   int
	LowConfidenceSummaryHitLimit int
}

// AppConfig 统一描述脚本与服务端共用配置，降低多入口行为漂移风险。
type AppConfig struct {
	ConfigPath       string
	MemoryRoot       string
	ServerBaseURL    string
	ServerListenAddr string
	JWTSecret        string
	DatabaseConfig   *DatabaseConfig
	EmbeddingConfig  *EmbeddingConfig
	SearchConfig     *SearchConfig
}

var (
	serverSectionKeys                        = []string{"server"}
	serverBaseURLKeys                        = []string{"base_url", "baseUrl", "url", "address"}
	serverListenAddrKeys                     = []string{"listen_addr", "listenAddr", "listen_address", "listenAddress", "bind", "bind_addr", "bindAddr"}
	serverFlatURLKeys                        = []string{"server_url", "serverUrl", "service_url", "serviceUrl"}
	serverFlatListenKeys                     = []string{"server_listen_addr", "serverListenAddr", "listen_addr", "listenAddr"}
	embeddingSectionKeys                     = []string{"embedding", "embeddings"}
	embeddingBaseURLKeys                     = []string{"base_url", "baseUrl", "url", "endpoint"}
	embeddingAPIKeyKeys                      = []string{"api_key", "apiKey"}
	embeddingModelKeys                       = []string{"model", "embedding_model", "embeddingModel"}
	embeddingTimeoutKeys                     = []string{"timeout_seconds", "timeoutSeconds"}
	embeddingSemanticSimilarityThresholdKeys = []string{"semantic_similarity_threshold", "semanticSimilarityThreshold"}
	embeddingSemanticCandidateBatchSizeKeys  = []string{"semantic_candidate_batch_size", "semanticCandidateBatchSize"}
	embeddingSemanticCandidateMaxCountKeys   = []string{"semantic_candidate_max_count", "semanticCandidateMaxCount"}
	embeddingSemanticHitFetchLimitKeys       = []string{"semantic_hit_fetch_limit", "semanticHitFetchLimit"}
	authSectionKeys                          = []string{"auth"}
	authJWTSecretKeys                        = []string{"jwt_secret", "jwtSecret"}
	databaseSectionKeys                      = []string{"database", "db"}
	databaseDriverKeys                       = []string{"driver", "dialect", "type"}
	databaseDSNKeys                          = []string{"dsn", "url", "uri"}
	databaseFlatDriver                       = []string{"database_driver", "databaseDriver", "db_driver", "dbDriver"}
	databaseFlatDSN                          = []string{"database_dsn", "databaseDsn", "db_dsn", "dbDsn", "database_url", "databaseUrl"}
	searchSectionKeys                        = []string{"search"}
	searchLowConfidenceErrorHitLimitKeys     = []string{"low_confidence_error_hit_limit", "lowConfidenceErrorHitLimit", "error_hit_limit", "errorHitLimit"}
	searchLowConfidenceSummaryHitLimitKeys   = []string{"low_confidence_summary_hit_limit", "lowConfidenceSummaryHitLimit", "summary_hit_limit", "summaryHitLimit"}
)

// Load 读取并标准化配置，缺失时自动补默认配置降低首次使用门槛。
func Load() (AppConfig, error) {
	configPath, err := defaultConfigPath()
	if err != nil {
		return AppConfig{}, err
	}
	payload, err := loadPayload(configPath)
	if err != nil {
		return AppConfig{}, err
	}
	memoryRoot, err := defaultMemoryRoot(configPath)
	if err != nil {
		return AppConfig{}, err
	}
	config := AppConfig{
		ConfigPath:       configPath,
		MemoryRoot:       memoryRoot,
		ServerBaseURL:    resolveServerBaseURL(payload),
		ServerListenAddr: resolveServerListenAddr(payload),
		JWTSecret:        resolveJWTSecret(payload),
		DatabaseConfig:   resolveDatabaseConfig(payload),
	}
	config.EmbeddingConfig = resolveEmbeddingConfig(payload)
	config.SearchConfig = resolveSearchConfig(payload)
	return config, nil
}

// defaultConfigPath 固定读取当前工作目录下的配置，避免服务端再受用户目录配置干扰。
func defaultConfigPath() (string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(workdir, "config.json"), nil
}

// defaultMemoryRoot 固定把服务端数据库放在配置文件同级目录下，保证服务只维护一份记忆库。
func defaultMemoryRoot(configPath string) (string, error) {
	resolved, err := filepath.Abs(filepath.Join(filepath.Dir(configPath), ".memory"))
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// loadPayload 负责读取配置文件并在缺失时补默认模板，减少首次运行阻塞。
func loadPayload(configPath string) (map[string]any, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		payload, buildErr := buildDefaultPayload()
		if buildErr != nil {
			return nil, buildErr
		}
		if writeErr := writeDefaultPayload(configPath, payload); writeErr == nil {
			return payload, nil
		}
		return payload, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// buildDefaultPayload 构造统一默认配置，避免服务端首次启动时必须手工建文件。
func buildDefaultPayload() (map[string]any, error) {
	return map[string]any{
		"server": map[string]any{
			"base_url":    defaultServerBaseURL,
			"listen_addr": defaultServerListenAddr,
		},
		"embedding": map[string]any{
			"base_url":                      "",
			"api_key":                       "",
			"model":                         "",
			"timeout_seconds":               30,
			"semantic_similarity_threshold": defaultSemanticSimilarityThreshold,
			"semantic_candidate_batch_size": defaultSemanticCandidateBatchSize,
			"semantic_candidate_max_count":  defaultSemanticCandidateMaxCount,
			"semantic_hit_fetch_limit":      defaultSemanticHitFetchLimit,
		},
		"database": map[string]any{
			"driver": "sqlite",
			"dsn":    "",
		},
		"search": map[string]any{
			"low_confidence_error_hit_limit":   defaultSearchErrorHitLimit,
			"low_confidence_summary_hit_limit": defaultSearchSummaryHitLimit,
		},
		"auth": map[string]any{
			"jwt_secret": defaultJWTSecret,
		},
	}, nil
}

// writeDefaultPayload 在配置缺失时补默认模板，避免用户必须先手工创建文件。
func writeDefaultPayload(configPath string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, append(data, '\n'), 0o644)
}

// resolveServerBaseURL 统一解析服务端地址，确保脚本侧 HTTP 调用入口稳定。
func resolveServerBaseURL(payload map[string]any) string {
	for _, key := range serverSectionKeys {
		section, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if value := pickStrings(section, serverBaseURLKeys); value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	if value := pickStrings(payload, serverFlatURLKeys); value != "" {
		return strings.TrimRight(value, "/")
	}
	return defaultServerBaseURL
}

// resolveServerListenAddr 统一解析服务端监听地址，避免服务入口继续写死端口。
func resolveServerListenAddr(payload map[string]any) string {
	for _, key := range serverSectionKeys {
		section, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if value := pickStrings(section, serverListenAddrKeys); value != "" {
			return value
		}
	}
	if value := pickStrings(payload, serverFlatListenKeys); value != "" {
		return value
	}
	return defaultServerListenAddr
}

// resolveEmbeddingConfig 只要求地址和模型存在，兼容本地 Ollama 这类无需鉴权的嵌入服务。
func resolveEmbeddingConfig(payload map[string]any) *EmbeddingConfig {
	section := findSection(payload, embeddingSectionKeys)
	baseURL := strings.TrimRight(pickStrings(section, embeddingBaseURLKeys), "/")
	apiKey := pickStrings(section, embeddingAPIKeyKeys)
	model := pickStrings(section, embeddingModelKeys)
	if baseURL == "" || model == "" {
		return nil
	}
	timeout := pickFloat(section, embeddingTimeoutKeys, 30)
	if timeout < 1 {
		timeout = 1
	}
	semanticSimilarityThreshold := pickFloat(section, embeddingSemanticSimilarityThresholdKeys, defaultSemanticSimilarityThreshold)
	if semanticSimilarityThreshold <= 0 {
		semanticSimilarityThreshold = defaultSemanticSimilarityThreshold
	}
	semanticCandidateBatchSize := pickInt(section, embeddingSemanticCandidateBatchSizeKeys, defaultSemanticCandidateBatchSize)
	if semanticCandidateBatchSize < 1 {
		semanticCandidateBatchSize = defaultSemanticCandidateBatchSize
	}
	semanticCandidateMaxCount := pickInt(section, embeddingSemanticCandidateMaxCountKeys, defaultSemanticCandidateMaxCount)
	if semanticCandidateMaxCount < semanticCandidateBatchSize {
		semanticCandidateMaxCount = semanticCandidateBatchSize
	}
	semanticHitFetchLimit := pickInt(section, embeddingSemanticHitFetchLimitKeys, defaultSemanticHitFetchLimit)
	if semanticHitFetchLimit < 1 {
		semanticHitFetchLimit = defaultSemanticHitFetchLimit
	}
	if semanticHitFetchLimit > semanticCandidateMaxCount {
		semanticHitFetchLimit = semanticCandidateMaxCount
	}
	return &EmbeddingConfig{
		BaseURL:                     baseURL,
		APIKey:                      apiKey,
		Model:                       model,
		TimeoutSeconds:              timeout,
		SemanticSimilarityThreshold: semanticSimilarityThreshold,
		SemanticCandidateBatchSize:  semanticCandidateBatchSize,
		SemanticCandidateMaxCount:   semanticCandidateMaxCount,
		SemanticHitFetchLimit:       semanticHitFetchLimit,
	}
}

// resolveJWTSecret 统一读取 JWT 密钥，避免管理接口鉴权在不同入口出现不一致的签名结果。
func resolveJWTSecret(payload map[string]any) string {
	section := findSection(payload, authSectionKeys)
	if value := pickStrings(section, authJWTSecretKeys); value != "" {
		return value
	}
	return defaultJWTSecret
}

// resolveDatabaseConfig 统一解析数据库配置，未配置时继续沿用默认 SQLite 行为。
func resolveDatabaseConfig(payload map[string]any) *DatabaseConfig {
	section := findSection(payload, databaseSectionKeys)
	driver := pickStrings(section, databaseDriverKeys)
	if driver == "" {
		driver = pickStrings(payload, databaseFlatDriver)
	}
	dsn := pickStrings(section, databaseDSNKeys)
	if dsn == "" {
		dsn = pickStrings(payload, databaseFlatDSN)
	}
	if driver == "" && dsn == "" {
		return nil
	}
	return &DatabaseConfig{Driver: driver, DSN: dsn}
}

// resolveSearchConfig 统一解析搜索返回上限，确保所有入口都遵循同一结果裁剪策略。
func resolveSearchConfig(payload map[string]any) *SearchConfig {
	section := findSection(payload, searchSectionKeys)
	errorLimit := pickInt(section, searchLowConfidenceErrorHitLimitKeys, defaultSearchErrorHitLimit)
	if errorLimit < 0 {
		errorLimit = 0
	}
	summaryLimit := pickInt(section, searchLowConfidenceSummaryHitLimitKeys, defaultSearchSummaryHitLimit)
	if summaryLimit < 0 {
		summaryLimit = 0
	}
	return &SearchConfig{LowConfidenceErrorHitLimit: errorLimit, LowConfidenceSummaryHitLimit: summaryLimit}
}

// findSection 优先读取嵌套配置，必要时兼容平铺结构减少升级摩擦。
func findSection(payload map[string]any, keys []string) map[string]any {
	for _, key := range keys {
		section, ok := payload[key].(map[string]any)
		if ok {
			return section
		}
	}
	return payload
}

// pickStrings 从候选字段中取第一个非空字符串，避免调用方重复写兼容逻辑。
func pickStrings(payload map[string]any, keys []string) string {
	for _, key := range keys {
		if value := pickString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

// pickString 单点读取字符串字段，避免大量不安全类型断言分散在业务代码里。
func pickString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// pickFloat 宽松解析数值，避免配置格式变化导致整个能力失效。
func pickFloat(payload map[string]any, keys []string, fallback float64) float64 {
	for _, key := range keys {
		if payload == nil {
			break
		}
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed
		case float32:
			return float64(typed)
		case int:
			return float64(typed)
		case int64:
			return float64(typed)
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

// pickInt 宽松解析整数配置，避免数值类开关因 JSON 类型差异失效。
func pickInt(payload map[string]any, keys []string, fallback int) int {
	for _, key := range keys {
		if payload == nil {
			break
		}
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case float32:
			return int(typed)
		case int:
			return typed
		case int64:
			return int(typed)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

// String 方便调试输出配置摘要，避免直接暴露完整敏感配置内容。
func (c AppConfig) String() string {
	driver := "sqlite"
	if c.DatabaseConfig != nil && strings.TrimSpace(c.DatabaseConfig.Driver) != "" {
		driver = strings.TrimSpace(c.DatabaseConfig.Driver)
	}
	return fmt.Sprintf("config=%s memory=%s server=%s listen=%s db=%s", c.ConfigPath, c.MemoryRoot, c.ServerBaseURL, c.ServerListenAddr, driver)
}
