package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultServerBaseURL               = "http://127.0.0.1:8080"
	defaultServerListenAddr            = ":8080"
	defaultJWTSecret                   = "hive-change-me"
	defaultSearchErrorHitLimit         = 10
	defaultSearchSummaryHitLimit       = 10
	defaultSemanticWindowMode          = "static"
	defaultSemanticWindowDynamicRatio  = 0.2
	defaultSemanticWindowDynamicMin    = 256
	defaultSemanticWindowDynamicMax    = 20000
	defaultSemanticWindowReferenceSize = 10000
	defaultDecayEnabled                = true
	defaultDecayAgeWeight              = 0.5
	defaultDecaySemanticWeight         = 0.5
	defaultDecaySummaryHalfLifeDays    = 30
	defaultDecayErrorHalfLifeDays      = 90
	defaultKeywordMode                 = "like"
	defaultKeywordBackend              = "auto"
	defaultKeywordSynonymsEnabled      = true
	defaultKeywordBM25K1               = 1.2
	defaultKeywordBM25B                = 0.75
	defaultFusionEnabled               = true
	defaultFusionFormula               = "weighted_sum"
	defaultFusionKeywordWeight         = 0.55
	defaultFusionSemanticWeight        = 0.45
	defaultFusionRecencyWeight         = 0.10
	defaultCacheEnabled                = false
	defaultCacheQueryEmbeddingTTL      = 600
	defaultCacheSemanticHitsTTL        = 120
	defaultCacheMaxEntries             = 5000
	defaultCacheStatsRefreshInterval   = 10
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
	SemanticWindowMode          string
	SemanticWindowBaseMaxCount  int
	SemanticWindowDynamicMin    int
	SemanticWindowDynamicMax    int
	SemanticWindowDynamicRatio  float64
	SemanticWindowReferenceSize int
	DecayEnabled                bool
	DecayAgeWeight              float64
	DecaySemanticWeight         float64
	DecaySummaryHalfLifeDays    float64
	DecayErrorHalfLifeDays      float64
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
	KeywordMode                  string
	KeywordBackend               string
	KeywordFields                []string
	KeywordFieldWeights          map[string]float64
	KeywordSynonymsEnabled       bool
	KeywordSynonymGroups         [][]string
	KeywordBM25K1                float64
	KeywordBM25B                 float64
	FusionEnabled                bool
	FusionFormula                string
	FusionKeywordWeight          float64
	FusionSemanticWeight         float64
	FusionRecencyWeight          float64
	FusionMinSemanticScore       float64
	CacheEnabled                 bool
	CacheQueryEmbeddingTTL       int
	CacheSemanticHitsTTL         int
	CacheMaxEntries              int
	CacheStatsRefreshInterval    int
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
	serverSectionKeys                        = "server"
	serverBaseURLKeys                        = "baseUrl"
	serverListenAddrKeys                     = "listenAddr"
	embeddingSectionKeys                     = "embedding"
	embeddingBaseURLKeys                     = "baseUrl"
	embeddingAPIKeyKeys                      = "apiKey"
	embeddingModelKeys                       = "model"
	embeddingTimeoutKeys                     = "timeoutSeconds"
	embeddingSemanticSimilarityThresholdKeys = "semanticSimilarityThreshold"
	embeddingSemanticCandidateBatchSizeKeys  = "semanticCandidateBatchSize"
	embeddingSemanticCandidateMaxCountKeys   = "semanticCandidateMaxCount"
	embeddingSemanticHitFetchLimitKeys       = "semanticHitFetchLimit"
	embeddingSemanticWindowSectionKeys       = "semanticWindow"
	embeddingDecaySectionKeys                = "decay"
	embeddingSemanticWindowModeKeys          = "mode"
	embeddingSemanticWindowBaseMaxCountKeys  = "baseMaxCount"
	embeddingSemanticWindowDynamicMinKeys    = "dynamicMinCount"
	embeddingSemanticWindowDynamicMaxKeys    = "dynamicMaxCount"
	embeddingSemanticWindowDynamicRatioKeys  = "dynamicRatio"
	embeddingSemanticWindowReferenceSizeKeys = "referenceCorpusSize"
	embeddingDecayEnabledKeys                = "enabled"
	embeddingDecayAgeWeightKeys              = "ageWeight"
	embeddingDecaySemanticWeightKeys         = "semanticWeight"
	embeddingDecayHalfLifeSectionKeys        = "halfLifeDays"
	embeddingDecaySummaryHalfLifeKeys        = "summary"
	embeddingDecayErrorHalfLifeKeys          = "error"
	authSectionKeys                          = "auth"
	authJWTSecretKeys                        = "jwtSecret"
	databaseSectionKeys                      = "database"
	databaseDriverKeys                       = "driver"
	databaseDSNKeys                          = "dsn"
	searchSectionKeys                        = "search"
	searchLowConfidenceErrorHitLimitKeys     = "lowConfidenceErrorHitLimit"
	searchLowConfidenceSummaryHitLimitKeys   = "lowConfidenceSummaryHitLimit"
	searchKeywordSectionKeys                 = "keyword"
	searchKeywordModeKeys                    = "mode"
	searchKeywordBackendKeys                 = "backend"
	searchKeywordFieldsKeys                  = "fields"
	searchKeywordFieldWeightsKeys            = "fieldWeights"
	searchKeywordSynonymsSectionKeys         = "synonyms"
	searchKeywordSynonymsEnabledKeys         = "enabled"
	searchKeywordSynonymsGroupsKeys          = "groups"
	searchKeywordBM25K1Keys                  = "bm25K1"
	searchKeywordBM25BKeys                   = "bm25B"
	searchFusionSectionKeys                  = "fusion"
	searchFusionEnabledKeys                  = "enabled"
	searchFusionFormulaKeys                  = "formula"
	searchFusionKeywordWeightKeys            = "keywordWeight"
	searchFusionSemanticWeightKeys           = "semanticWeight"
	searchFusionRecencyWeightKeys            = "recencyWeight"
	searchFusionMinSemanticScoreKeys         = "minSemanticScore"
	searchCacheSectionKeys                   = "cache"
	searchCacheEnabledKeys                   = "enabled"
	searchCacheQueryEmbeddingTTLKeys         = "queryEmbeddingTtlSeconds"
	searchCacheSemanticHitsTTLKeys           = "semanticHitsTtlSeconds"
	searchCacheMaxEntriesKeys                = "maxEntries"
	searchCacheStatsRefreshIntervalKeys      = "statsRefreshIntervalSeconds"
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
	payload, err := unmarshalJSONCPayload(data)
	if err != nil {
		return nil, err
	}
	defaults, err := buildDefaultPayload()
	if err != nil {
		return nil, err
	}
	mergedPayload, changed := mergeMissingDefaults(payload, defaults)
	if changed {
		if writeErr := writeDefaultPayload(configPath, mergedPayload); writeErr == nil {
			return mergedPayload, nil
		}
	}
	return mergedPayload, nil
}

// buildDefaultPayload 构造统一默认配置，避免服务端首次启动时必须手工建文件。
func buildDefaultPayload() (map[string]any, error) {
	return map[string]any{
		"server": map[string]any{
			"baseUrl":    defaultServerBaseURL,
			"listenAddr": defaultServerListenAddr,
		},
		"embedding": map[string]any{
			"baseUrl":                     "",
			"apiKey":                      "",
			"model":                       "",
			"timeoutSeconds":              30,
			"semanticSimilarityThreshold": defaultSemanticSimilarityThreshold,
			"semanticCandidateBatchSize":  defaultSemanticCandidateBatchSize,
			"semanticCandidateMaxCount":   defaultSemanticCandidateMaxCount,
			"semanticHitFetchLimit":       defaultSemanticHitFetchLimit,
			"semanticWindow": map[string]any{
				"mode":                defaultSemanticWindowMode,
				"baseMaxCount":        defaultSemanticCandidateMaxCount,
				"dynamicMinCount":     defaultSemanticWindowDynamicMin,
				"dynamicMaxCount":     defaultSemanticWindowDynamicMax,
				"dynamicRatio":        defaultSemanticWindowDynamicRatio,
				"referenceCorpusSize": defaultSemanticWindowReferenceSize,
			},
			"decay": map[string]any{
				"enabled":        defaultDecayEnabled,
				"ageWeight":      defaultDecayAgeWeight,
				"semanticWeight": defaultDecaySemanticWeight,
				"halfLifeDays": map[string]any{
					"summary": defaultDecaySummaryHalfLifeDays,
					"error":   defaultDecayErrorHalfLifeDays,
				},
			},
		},
		"database": map[string]any{
			"driver": "sqlite",
			"dsn":    "",
		},
		"search": map[string]any{
			"lowConfidenceErrorHitLimit":   defaultSearchErrorHitLimit,
			"lowConfidenceSummaryHitLimit": defaultSearchSummaryHitLimit,
			"keyword": map[string]any{
				"mode":    defaultKeywordMode,
				"backend": defaultKeywordBackend,
				"bm25K1":  defaultKeywordBM25K1,
				"bm25B":   defaultKeywordBM25B,
				"fields":  []string{"title", "summary", "tags", "content"},
				"fieldWeights": map[string]any{
					"title":   2.0,
					"summary": 1.5,
					"tags":    1.5,
					"content": 1.0,
				},
				"synonyms": map[string]any{
					"enabled": defaultKeywordSynonymsEnabled,
					"groups":  [][]string{},
				},
			},
			"fusion": map[string]any{
				"enabled":          defaultFusionEnabled,
				"formula":          defaultFusionFormula,
				"keywordWeight":    defaultFusionKeywordWeight,
				"semanticWeight":   defaultFusionSemanticWeight,
				"recencyWeight":    defaultFusionRecencyWeight,
				"minSemanticScore": defaultSemanticSimilarityThreshold,
			},
			"cache": map[string]any{
				"enabled":                     defaultCacheEnabled,
				"queryEmbeddingTtlSeconds":    defaultCacheQueryEmbeddingTTL,
				"semanticHitsTtlSeconds":      defaultCacheSemanticHitsTTL,
				"maxEntries":                  defaultCacheMaxEntries,
				"statsRefreshIntervalSeconds": defaultCacheStatsRefreshInterval,
			},
		},
		"auth": map[string]any{
			"jwtSecret": defaultJWTSecret,
		},
	}, nil
}

// writeDefaultPayload 在配置缺失时补默认模板，避免用户必须先手工创建文件。
func writeDefaultPayload(configPath string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, err := marshalJSONCWithComments(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0o644)
}

// unmarshalJSONCPayload 支持解析 JSONC 配置，避免注释导致配置加载失败。
func unmarshalJSONCPayload(data []byte) (map[string]any, error) {
	cleaned := stripJSONCComments(string(data))
	var payload map[string]any
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

// mergeMissingDefaults 仅补齐缺失键，避免升级配置时覆盖用户已显式设置的值。
func mergeMissingDefaults(payload, defaults map[string]any) (map[string]any, bool) {
	if payload == nil {
		payload = map[string]any{}
	}
	changed := false
	for key, defaultValue := range defaults {
		existingValue, ok := payload[key]
		if !ok {
			payload[key] = cloneConfigValue(defaultValue)
			changed = true
			continue
		}
		existingMap, existingIsMap := toStringAnyMap(existingValue)
		defaultMap, defaultIsMap := toStringAnyMap(defaultValue)
		if existingIsMap && defaultIsMap {
			merged, nestedChanged := mergeMissingDefaults(existingMap, defaultMap)
			if nestedChanged {
				changed = true
			}
			payload[key] = merged
		}
	}
	return payload, changed
}

// cloneConfigValue 深拷贝默认配置值，避免不同层级共享底层引用导致串改。
func cloneConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneConfigValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneConfigValue(item))
		}
		return cloned
	case []string:
		cloned := make([]string, len(typed))
		copy(cloned, typed)
		return cloned
	case [][]string:
		cloned := make([][]string, 0, len(typed))
		for _, group := range typed {
			inner := make([]string, len(group))
			copy(inner, group)
			cloned = append(cloned, inner)
		}
		return cloned
	default:
		return typed
	}
}

// toStringAnyMap 兼容 map[string]any 与 map[any]any，避免不同解码路径造成类型分支遗漏。
func toStringAnyMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		converted := map[string]any{}
		for key, item := range typed {
			converted[strings.TrimSpace(fmt.Sprint(key))] = item
		}
		return converted, true
	default:
		return nil, false
	}
}

// marshalJSONCWithComments 生成带说明注释的 JSONC，帮助用户理解每个配置项的用途。
func marshalJSONCWithComments(payload map[string]any) ([]byte, error) {
	rendered, err := renderConfigJSONCValue(payload, "", 0)
	if err != nil {
		return nil, err
	}
	return []byte(rendered + "\n"), nil
}

// renderConfigJSONCValue 递归渲染 JSONC，并把说明注释放在每个配置项后面。
func renderConfigJSONCValue(value any, parentPath string, indentLevel int) (string, error) {
	switch typed := value.(type) {
	case map[string]any:
		return renderConfigJSONCObject(typed, parentPath, indentLevel)
	case []any:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case []string:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case [][]string:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// renderConfigJSONCObject 渲染对象并在每个键值后附带注释，便于用户就地理解配置语义。
func renderConfigJSONCObject(obj map[string]any, parentPath string, indentLevel int) (string, error) {
	indent := strings.Repeat("  ", indentLevel)
	childIndent := strings.Repeat("  ", indentLevel+1)
	keys := orderedConfigKeys(parentPath, obj)
	if len(keys) == 0 {
		return "{}", nil
	}
	lines := []string{"{"}
	for idx, key := range keys {
		fullPath := key
		if parentPath != "" {
			fullPath = parentPath + "." + key
		}
		renderedValue, err := renderConfigJSONCValue(obj[key], fullPath, indentLevel+1)
		if err != nil {
			return "", err
		}
		line := childIndent + strconv.Quote(key) + ": " + renderedValue
		if idx < len(keys)-1 {
			line += ","
		}
		if comment := configCommentForPath(fullPath); comment != "" {
			line += " // " + comment
		}
		lines = append(lines, line)
	}
	lines = append(lines, indent+"}")
	return strings.Join(lines, "\n"), nil
}

// orderedConfigKeys 按固定顺序输出键，避免配置文件每次自动补齐后顺序漂移影响可读性。
func orderedConfigKeys(parentPath string, obj map[string]any) []string {
	preferred := map[string][]string{
		"":                             {"server", "auth", "database", "search", "embedding"},
		"server":                       {"baseUrl", "listenAddr"},
		"auth":                         {"jwtSecret"},
		"database":                     {"driver", "dsn"},
		"search":                       {"lowConfidenceErrorHitLimit", "lowConfidenceSummaryHitLimit", "keyword", "fusion", "cache"},
		"search.keyword":               {"mode", "backend", "bm25K1", "bm25B", "fields", "fieldWeights", "synonyms"},
		"search.keyword.fieldWeights":  {"title", "summary", "tags", "content", "project_name"},
		"search.keyword.synonyms":      {"enabled", "groups"},
		"search.fusion":                {"enabled", "formula", "keywordWeight", "semanticWeight", "recencyWeight", "minSemanticScore"},
		"search.cache":                 {"enabled", "queryEmbeddingTtlSeconds", "semanticHitsTtlSeconds", "maxEntries", "statsRefreshIntervalSeconds"},
		"embedding":                    {"baseUrl", "apiKey", "model", "timeoutSeconds", "semanticSimilarityThreshold", "semanticCandidateBatchSize", "semanticCandidateMaxCount", "semanticHitFetchLimit", "semanticWindow", "decay"},
		"embedding.semanticWindow":     {"mode", "baseMaxCount", "dynamicMinCount", "dynamicMaxCount", "dynamicRatio", "referenceCorpusSize"},
		"embedding.decay":              {"enabled", "ageWeight", "semanticWeight", "halfLifeDays"},
		"embedding.decay.halfLifeDays": {"summary", "error"},
	}
	ordered := make([]string, 0, len(obj))
	used := map[string]struct{}{}
	if expected, ok := preferred[parentPath]; ok {
		for _, key := range expected {
			if _, exists := obj[key]; exists {
				ordered = append(ordered, key)
				used[key] = struct{}{}
			}
		}
	}
	extra := make([]string, 0, len(obj))
	for key := range obj {
		if _, ok := used[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	ordered = append(ordered, extra...)
	return ordered
}

// configCommentForPath 返回配置项说明，确保注释跟随每一项输出而不是集中在文件顶部。
func configCommentForPath(path string) string {
	comments := map[string]string{
		"server":                                       "服务端监听与对外访问配置",
		"server.baseUrl":                               "服务端对外访问地址，客户端会以此作为 API 入口",
		"server.listenAddr":                            "服务端本地监听地址",
		"auth":                                         "鉴权相关配置",
		"auth.jwtSecret":                               "管理后台 JWT 签名密钥，生产环境应替换",
		"database":                                     "数据库连接配置",
		"database.driver":                              "数据库驱动，支持 sqlite/postgres/postgresql",
		"database.dsn":                                 "数据库连接串，sqlite 为空时使用默认本地文件",
		"search":                                       "搜索层配置",
		"search.lowConfidenceErrorHitLimit":            "错误记忆中低于 1 分置信度的最大返回条数",
		"search.lowConfidenceSummaryHitLimit":          "总结记忆中低于 1 分置信度的最大返回条数",
		"search.keyword":                               "关键字检索配置",
		"search.keyword.mode":                          "关键字模式，like 为子串匹配，bm25 为加权相关性排序",
		"search.keyword.backend":                       "关键字后端类型预留项，默认 auto",
		"search.keyword.bm25K1":                        "BM25 的 k1 参数，控制词频饱和速度",
		"search.keyword.bm25B":                         "BM25 的 b 参数，控制文档长度归一化强度",
		"search.keyword.fields":                        "BM25 参与打分字段列表",
		"search.keyword.fieldWeights":                  "BM25 各字段权重映射",
		"search.keyword.fieldWeights.title":            "标题字段权重",
		"search.keyword.fieldWeights.summary":          "摘要字段权重",
		"search.keyword.fieldWeights.tags":             "标签字段权重",
		"search.keyword.fieldWeights.content":          "正文字段权重",
		"search.keyword.fieldWeights.project_name":     "项目名字段权重",
		"search.keyword.synonyms":                      "同义词扩展配置",
		"search.keyword.synonyms.enabled":              "是否启用同义词扩展",
		"search.keyword.synonyms.groups":               "同义词分组，每组内词会互相扩展",
		"search.fusion":                                "多路打分融合配置",
		"search.fusion.enabled":                        "是否启用关键字/语义/时效融合评分",
		"search.fusion.formula":                        "融合公式，当前支持 weighted_sum",
		"search.fusion.keywordWeight":                  "关键字分在融合中的权重",
		"search.fusion.semanticWeight":                 "语义分在融合中的权重",
		"search.fusion.recencyWeight":                  "时效分在融合中的权重",
		"search.fusion.minSemanticScore":               "语义分最低有效阈值，低于该值会被视为弱语义",
		"search.cache":                                 "搜索缓存配置",
		"search.cache.enabled":                         "是否启用查询向量与语义结果缓存",
		"search.cache.queryEmbeddingTtlSeconds":        "查询向量缓存 TTL（秒）",
		"search.cache.semanticHitsTtlSeconds":          "语义命中缓存 TTL（秒）",
		"search.cache.maxEntries":                      "每类缓存的最大条目数",
		"search.cache.statsRefreshIntervalSeconds":     "管理端缓存统计刷新间隔（秒），0 表示仅手动刷新",
		"embedding":                                    "嵌入与语义召回配置",
		"embedding.baseUrl":                            "OpenAI 兼容 Embeddings 服务地址",
		"embedding.apiKey":                             "Embeddings 服务鉴权令牌",
		"embedding.model":                              "嵌入模型名称",
		"embedding.timeoutSeconds":                     "嵌入请求超时时间（秒）",
		"embedding.semanticSimilarityThreshold":        "语义命中阈值，低于该值不进入候选",
		"embedding.semanticCandidateBatchSize":         "每批扫描的语义候选数量",
		"embedding.semanticCandidateMaxCount":          "语义扫描候选总上限",
		"embedding.semanticHitFetchLimit":              "语义高分候选回表上限",
		"embedding.semanticWindow":                     "语义候选窗口策略",
		"embedding.semanticWindow.mode":                "窗口模式，static 固定窗口，dynamic 按语料规模动态计算",
		"embedding.semanticWindow.baseMaxCount":        "静态模式窗口上限，动态模式下作为兜底值",
		"embedding.semanticWindow.dynamicMinCount":     "动态窗口最小值",
		"embedding.semanticWindow.dynamicMaxCount":     "动态窗口最大值",
		"embedding.semanticWindow.dynamicRatio":        "动态窗口比例因子（候选数≈语料量*比例）",
		"embedding.semanticWindow.referenceCorpusSize": "动态窗口参考语料规模预留项",
		"embedding.decay":                              "时效衰减配置",
		"embedding.decay.enabled":                      "是否启用时间衰减融合",
		"embedding.decay.ageWeight":                    "时效分权重",
		"embedding.decay.semanticWeight":               "语义分权重",
		"embedding.decay.halfLifeDays":                 "不同记忆类型的半衰期配置（天）",
		"embedding.decay.halfLifeDays.summary":         "总结记忆半衰期（天）",
		"embedding.decay.halfLifeDays.error":           "错误记忆半衰期（天）",
	}
	return comments[path]
}

// stripJSONCComments 在保留字符串字面量的前提下移除 JSONC 注释，避免 http:// 这类内容被误删。
func stripJSONCComments(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false
	for idx := 0; idx < len(input); idx++ {
		current := input[idx]
		next := byte(0)
		if idx+1 < len(input) {
			next = input[idx+1]
		}
		if inLineComment {
			if current == '\n' {
				inLineComment = false
				builder.WriteByte(current)
			}
			continue
		}
		if inBlockComment {
			if current == '*' && next == '/' {
				inBlockComment = false
				idx++
			}
			continue
		}
		if inString {
			builder.WriteByte(current)
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			builder.WriteByte(current)
			continue
		}
		if current == '/' && next == '/' {
			inLineComment = true
			idx++
			continue
		}
		if current == '/' && next == '*' {
			inBlockComment = true
			idx++
			continue
		}
		builder.WriteByte(current)
	}
	return builder.String()
}

// resolveServerBaseURL 统一解析服务端地址，确保脚本侧 HTTP 调用入口稳定。
func resolveServerBaseURL(payload map[string]any) string {
	section, ok := payload[serverSectionKeys].(map[string]any)
	if ok {
		if value := pickStrings(section, serverBaseURLKeys); value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	return defaultServerBaseURL
}

// resolveServerListenAddr 统一解析服务端监听地址，避免服务入口继续写死端口。
func resolveServerListenAddr(payload map[string]any) string {
	section, ok := payload[serverSectionKeys].(map[string]any)
	if ok {
		if value := pickStrings(section, serverListenAddrKeys); value != "" {
			return value
		}
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
	semanticWindow := findSection(section, embeddingSemanticWindowSectionKeys)
	semanticWindowMode := strings.ToLower(strings.TrimSpace(pickStrings(semanticWindow, embeddingSemanticWindowModeKeys)))
	if semanticWindowMode == "" {
		semanticWindowMode = defaultSemanticWindowMode
	}
	if semanticWindowMode != "dynamic" {
		semanticWindowMode = "static"
	}
	semanticWindowBaseMaxCount := pickInt(semanticWindow, embeddingSemanticWindowBaseMaxCountKeys, semanticCandidateMaxCount)
	if semanticWindowBaseMaxCount < 1 {
		semanticWindowBaseMaxCount = semanticCandidateMaxCount
	}
	semanticWindowDynamicMin := pickInt(semanticWindow, embeddingSemanticWindowDynamicMinKeys, defaultSemanticWindowDynamicMin)
	if semanticWindowDynamicMin < 1 {
		semanticWindowDynamicMin = 1
	}
	semanticWindowDynamicMax := pickInt(semanticWindow, embeddingSemanticWindowDynamicMaxKeys, defaultSemanticWindowDynamicMax)
	if semanticWindowDynamicMax < semanticWindowDynamicMin {
		semanticWindowDynamicMax = semanticWindowDynamicMin
	}
	semanticWindowDynamicRatio := pickFloat(semanticWindow, embeddingSemanticWindowDynamicRatioKeys, defaultSemanticWindowDynamicRatio)
	if semanticWindowDynamicRatio <= 0 {
		semanticWindowDynamicRatio = defaultSemanticWindowDynamicRatio
	}
	semanticWindowReferenceSize := pickInt(semanticWindow, embeddingSemanticWindowReferenceSizeKeys, defaultSemanticWindowReferenceSize)
	if semanticWindowReferenceSize < 1 {
		semanticWindowReferenceSize = defaultSemanticWindowReferenceSize
	}

	decay := findSection(section, embeddingDecaySectionKeys)
	decayEnabled := pickBool(decay, embeddingDecayEnabledKeys, defaultDecayEnabled)
	decayAgeWeight := pickFloat(decay, embeddingDecayAgeWeightKeys, defaultDecayAgeWeight)
	if decayAgeWeight < 0 {
		decayAgeWeight = 0
	}
	decaySemanticWeight := pickFloat(decay, embeddingDecaySemanticWeightKeys, defaultDecaySemanticWeight)
	if decaySemanticWeight < 0 {
		decaySemanticWeight = 0
	}
	totalDecayWeight := decayAgeWeight + decaySemanticWeight
	if totalDecayWeight <= 0 {
		decayAgeWeight = defaultDecayAgeWeight
		decaySemanticWeight = defaultDecaySemanticWeight
	}
	halfLife := findSection(decay, embeddingDecayHalfLifeSectionKeys)
	decaySummaryHalfLifeDays := pickFloat(halfLife, embeddingDecaySummaryHalfLifeKeys, defaultDecaySummaryHalfLifeDays)
	if decaySummaryHalfLifeDays <= 0 {
		decaySummaryHalfLifeDays = defaultDecaySummaryHalfLifeDays
	}
	decayErrorHalfLifeDays := pickFloat(halfLife, embeddingDecayErrorHalfLifeKeys, defaultDecayErrorHalfLifeDays)
	if decayErrorHalfLifeDays <= 0 {
		decayErrorHalfLifeDays = defaultDecayErrorHalfLifeDays
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
		SemanticWindowMode:          semanticWindowMode,
		SemanticWindowBaseMaxCount:  semanticWindowBaseMaxCount,
		SemanticWindowDynamicMin:    semanticWindowDynamicMin,
		SemanticWindowDynamicMax:    semanticWindowDynamicMax,
		SemanticWindowDynamicRatio:  semanticWindowDynamicRatio,
		SemanticWindowReferenceSize: semanticWindowReferenceSize,
		DecayEnabled:                decayEnabled,
		DecayAgeWeight:              decayAgeWeight,
		DecaySemanticWeight:         decaySemanticWeight,
		DecaySummaryHalfLifeDays:    decaySummaryHalfLifeDays,
		DecayErrorHalfLifeDays:      decayErrorHalfLifeDays,
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
	dsn := pickStrings(section, databaseDSNKeys)
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

	keywordSection := findSection(section, searchKeywordSectionKeys)
	keywordMode := strings.ToLower(strings.TrimSpace(pickStrings(keywordSection, searchKeywordModeKeys)))
	if keywordMode == "" {
		keywordMode = defaultKeywordMode
	}
	if keywordMode != "bm25" {
		keywordMode = "like"
	}
	keywordBackend := strings.ToLower(strings.TrimSpace(pickStrings(keywordSection, searchKeywordBackendKeys)))
	if keywordBackend == "" {
		keywordBackend = defaultKeywordBackend
	}
	keywordFields := pickStringSlice(keywordSection, searchKeywordFieldsKeys)
	if len(keywordFields) == 0 {
		keywordFields = []string{"title", "summary", "tags", "content"}
	}
	keywordFieldWeights := pickFloatMap(keywordSection, searchKeywordFieldWeightsKeys)
	if len(keywordFieldWeights) == 0 {
		keywordFieldWeights = map[string]float64{"title": 2.0, "summary": 1.5, "tags": 1.5, "content": 1.0}
	}
	keywordBM25K1 := pickFloat(keywordSection, searchKeywordBM25K1Keys, defaultKeywordBM25K1)
	if keywordBM25K1 <= 0 {
		keywordBM25K1 = defaultKeywordBM25K1
	}
	keywordBM25B := pickFloat(keywordSection, searchKeywordBM25BKeys, defaultKeywordBM25B)
	if keywordBM25B < 0 {
		keywordBM25B = 0
	}
	if keywordBM25B > 1 {
		keywordBM25B = 1
	}
	synonymsSection := findSection(keywordSection, searchKeywordSynonymsSectionKeys)
	keywordSynonymsEnabled := pickBool(synonymsSection, searchKeywordSynonymsEnabledKeys, defaultKeywordSynonymsEnabled)
	keywordSynonymGroups := pickStringGroups(synonymsSection, searchKeywordSynonymsGroupsKeys)

	fusionSection := findSection(section, searchFusionSectionKeys)
	fusionEnabled := pickBool(fusionSection, searchFusionEnabledKeys, defaultFusionEnabled)
	fusionFormula := strings.ToLower(strings.TrimSpace(pickStrings(fusionSection, searchFusionFormulaKeys)))
	if fusionFormula == "" {
		fusionFormula = defaultFusionFormula
	}
	if fusionFormula != "weighted_sum" {
		fusionFormula = "weighted_sum"
	}
	fusionKeywordWeight := pickFloat(fusionSection, searchFusionKeywordWeightKeys, defaultFusionKeywordWeight)
	if fusionKeywordWeight < 0 {
		fusionKeywordWeight = 0
	}
	fusionSemanticWeight := pickFloat(fusionSection, searchFusionSemanticWeightKeys, defaultFusionSemanticWeight)
	if fusionSemanticWeight < 0 {
		fusionSemanticWeight = 0
	}
	fusionRecencyWeight := pickFloat(fusionSection, searchFusionRecencyWeightKeys, defaultFusionRecencyWeight)
	if fusionRecencyWeight < 0 {
		fusionRecencyWeight = 0
	}
	fusionMinSemanticScore := pickFloat(fusionSection, searchFusionMinSemanticScoreKeys, defaultSemanticSimilarityThreshold)
	if fusionMinSemanticScore < 0 {
		fusionMinSemanticScore = 0
	}

	cacheSection := findSection(section, searchCacheSectionKeys)
	cacheEnabled := pickBool(cacheSection, searchCacheEnabledKeys, defaultCacheEnabled)
	cacheQueryEmbeddingTTL := pickInt(cacheSection, searchCacheQueryEmbeddingTTLKeys, defaultCacheQueryEmbeddingTTL)
	if cacheQueryEmbeddingTTL < 1 {
		cacheQueryEmbeddingTTL = 1
	}
	cacheSemanticHitsTTL := pickInt(cacheSection, searchCacheSemanticHitsTTLKeys, defaultCacheSemanticHitsTTL)
	if cacheSemanticHitsTTL < 1 {
		cacheSemanticHitsTTL = 1
	}
	cacheMaxEntries := pickInt(cacheSection, searchCacheMaxEntriesKeys, defaultCacheMaxEntries)
	if cacheMaxEntries < 1 {
		cacheMaxEntries = 1
	}
	cacheStatsRefreshInterval := pickInt(cacheSection, searchCacheStatsRefreshIntervalKeys, defaultCacheStatsRefreshInterval)
	if cacheStatsRefreshInterval < 0 {
		cacheStatsRefreshInterval = 0
	}

	return &SearchConfig{
		LowConfidenceErrorHitLimit:   errorLimit,
		LowConfidenceSummaryHitLimit: summaryLimit,
		KeywordMode:                  keywordMode,
		KeywordBackend:               keywordBackend,
		KeywordFields:                keywordFields,
		KeywordFieldWeights:          keywordFieldWeights,
		KeywordSynonymsEnabled:       keywordSynonymsEnabled,
		KeywordSynonymGroups:         keywordSynonymGroups,
		KeywordBM25K1:                keywordBM25K1,
		KeywordBM25B:                 keywordBM25B,
		FusionEnabled:                fusionEnabled,
		FusionFormula:                fusionFormula,
		FusionKeywordWeight:          fusionKeywordWeight,
		FusionSemanticWeight:         fusionSemanticWeight,
		FusionRecencyWeight:          fusionRecencyWeight,
		FusionMinSemanticScore:       fusionMinSemanticScore,
		CacheEnabled:                 cacheEnabled,
		CacheQueryEmbeddingTTL:       cacheQueryEmbeddingTTL,
		CacheSemanticHitsTTL:         cacheSemanticHitsTTL,
		CacheMaxEntries:              cacheMaxEntries,
		CacheStatsRefreshInterval:    cacheStatsRefreshInterval,
	}
}

// findSection 读取指定节配置，确保各模块按统一层级解析。

func findSection(payload map[string]any, key string) map[string]any {
	section, ok := payload[key].(map[string]any)
	if ok {
		return section
	}
	return payload
}

// pickStrings 从候选字段中取第一个非空字符串，避免调用方重复写兼容逻辑。
func pickStrings(payload map[string]any, key string) string {
	if value := pickString(payload, key); value != "" {
		return value
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

func pickFloat(payload map[string]any, key string, fallback float64) float64 {
	if payload == nil {
		return fallback
	}
	value, ok := payload[key]
	if !ok {
		return fallback
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
	return fallback
}

// pickInt 宽松解析整数配置，避免数值类开关因 JSON 类型差异失效。

func pickInt(payload map[string]any, key string, fallback int) int {
	if payload == nil {
		return fallback
	}
	value, ok := payload[key]
	if !ok {
		return fallback
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
	return fallback
}

// pickBool 宽松解析布尔配置，避免 true/false 的字符串写法导致配置失效。

func pickBool(payload map[string]any, key string, fallback bool) bool {
	if payload == nil {
		return fallback
	}
	value, ok := payload[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on" {
			return true
		}
		if normalized == "false" || normalized == "0" || normalized == "no" || normalized == "off" {
			return false
		}
	}
	return fallback
}

// pickStringSlice 宽松解析字符串数组，避免 JSON 写法差异影响字段列表配置。

func pickStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	out := []string{}
	switch typed := value.(type) {
	case []any:
		out = make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
	case []string:
		out = make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(item)
			if text != "" {
				out = append(out, text)
			}
		}
	default:
		return nil
	}
	if len(out) > 0 {
		return out
	}
	return nil
}

// pickFloatMap 宽松解析权重映射，避免数字类型差异导致字段权重被整体忽略。

func pickFloatMap(payload map[string]any, key string) map[string]float64 {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	out := map[string]float64{}
	switch typed := value.(type) {
	case map[string]any:
		for field, weightRaw := range typed {
			fieldName := strings.TrimSpace(field)
			if fieldName == "" {
				continue
			}
			weight := pickFloat(map[string]any{"value": weightRaw}, "value", 0)
			if weight <= 0 {
				continue
			}
			out[fieldName] = weight
		}
	case map[string]float64:
		for field, weight := range typed {
			fieldName := strings.TrimSpace(field)
			if fieldName == "" || weight <= 0 {
				continue
			}
			out[fieldName] = weight
		}
	default:
		return nil
	}
	if len(out) > 0 {
		return out
	}
	return nil
}

// pickStringGroups 宽松解析同义词分组，避免输入存在空白词时污染查询扩展结果。

func pickStringGroups(payload map[string]any, key string) [][]string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	groups := [][]string{}
	switch typed := value.(type) {
	case []any:
		groups = make([][]string, 0, len(typed))
		for _, groupRaw := range typed {
			inner, ok := groupRaw.([]any)
			if !ok {
				continue
			}
			group := make([]string, 0, len(inner))
			for _, item := range inner {
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" {
					group = append(group, text)
				}
			}
			if len(group) > 0 {
				groups = append(groups, group)
			}
		}
	case [][]string:
		groups = make([][]string, 0, len(typed))
		for _, rawGroup := range typed {
			group := make([]string, 0, len(rawGroup))
			for _, item := range rawGroup {
				text := strings.TrimSpace(item)
				if text != "" {
					group = append(group, text)
				}
			}
			if len(group) > 0 {
				groups = append(groups, group)
			}
		}
	default:
		return nil
	}
	if len(groups) > 0 {
		return groups
	}
	return nil
}

// String 方便调试输出配置摘要，避免直接暴露完整敏感配置内容。
func (c AppConfig) String() string {
	driver := "sqlite"
	if c.DatabaseConfig != nil && strings.TrimSpace(c.DatabaseConfig.Driver) != "" {
		driver = strings.TrimSpace(c.DatabaseConfig.Driver)
	}
	return fmt.Sprintf("config=%s memory=%s server=%s listen=%s db=%s", c.ConfigPath, c.MemoryRoot, c.ServerBaseURL, c.ServerListenAddr, driver)
}
