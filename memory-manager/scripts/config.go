package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// EmbeddingConfig 统一承载嵌入配置，避免不同命令各自解释字段产生偏差。
type EmbeddingConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	TimeoutSeconds float64
}

// MemoryLocation 统一描述当前项目的记忆落点，避免路径决策分散在多处。
type MemoryLocation struct {
	ProjectRoot     string
	ProjectName     string
	SearchRoot      string
	MemoryRoot      string
	ExternalEnabled bool
	ConfigPath      string
	EmbeddingConfig *EmbeddingConfig
}

var (
	configPathDefault  = filepath.Join(userHomeDir(), ".config", "memorymanager", "config.json")
	storageRootDefault = filepath.Join(userHomeDir(), ".local", "share", "memorymanager")
	storagePathKeys    = []string{"memory_storage_path", "memoryStorePath", "storage_path", "storagePath"}
	embeddingKeys      = []string{"embedding", "embeddings"}
	baseURLKeys        = []string{"base_url", "baseUrl", "url", "endpoint"}
	apiKeyKeys         = []string{"api_key", "apiKey"}
	modelKeys          = []string{"model", "embedding_model", "embeddingModel"}
	timeoutKeys        = []string{"timeout_seconds", "timeoutSeconds"}
)

// resolveMemoryLocation 优先使用项目内存储，缺失时再回退外挂目录，保证本地项目能自主管理数据。
func resolveMemoryLocation(projectRoot string) (MemoryLocation, error) {
	resolvedRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return MemoryLocation{}, err
	}
	config := loadConfig(configPathDefault)
	embeddingConfig := resolveEmbeddingConfig(config)
	localMemoryRoot := filepath.Join(resolvedRoot, ".memory")
	projectName := sanitizeProjectName(filepath.Base(resolvedRoot))

	if isDir(localMemoryRoot) {
		return MemoryLocation{
			ProjectRoot:     resolvedRoot,
			ProjectName:     projectName,
			SearchRoot:      resolvedRoot,
			MemoryRoot:      localMemoryRoot,
			ExternalEnabled: false,
			ConfigPath:      configPathDefault,
			EmbeddingConfig: embeddingConfig,
		}, nil
	}

	storageRoot := resolveStorageRoot(config, filepath.Dir(configPathDefault))
	if storageRoot != "" {
		return MemoryLocation{
			ProjectRoot:     resolvedRoot,
			ProjectName:     projectName,
			SearchRoot:      storageRoot,
			MemoryRoot:      filepath.Join(storageRoot, ".memory"),
			ExternalEnabled: true,
			ConfigPath:      configPathDefault,
			EmbeddingConfig: embeddingConfig,
		}, nil
	}

	return MemoryLocation{
		ProjectRoot:     resolvedRoot,
		ProjectName:     projectName,
		SearchRoot:      resolvedRoot,
		MemoryRoot:      localMemoryRoot,
		ExternalEnabled: false,
		ConfigPath:      configPathDefault,
		EmbeddingConfig: embeddingConfig,
	}, nil
}

// sanitizeProjectName 保证项目名稳定可检索，避免空值污染共享库隔离维度。
func sanitizeProjectName(name string) string {
	cleaned := strings.Trim(strings.TrimSpace(name), "./")
	if cleaned == "" {
		return "default-project"
	}
	return cleaned
}

// loadConfig 尽量返回可用配置，并在首次运行时自动写入默认文件降低使用门槛。
func loadConfig(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		defaultConfig := buildDefaultConfig()
		_ = writeDefaultConfig(path, defaultConfig)
		return defaultConfig
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return payload
}

// buildDefaultConfig 提供统一默认配置，避免不同命令首次执行时行为不一致。
func buildDefaultConfig() map[string]any {
	return map[string]any{
		"memory_storage_path": storageRootDefault,
		"embedding": map[string]any{
			"base_url":        "",
			"api_key":         "",
			"model":           "",
			"timeout_seconds": 30,
		},
	}
}

// writeDefaultConfig 在首次缺失配置时补齐模板，避免用户必须手写样板文件。
func writeDefaultConfig(path string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// resolveStorageRoot 兼容多个字段命名和相对路径，降低旧配置迁移成本。
func resolveStorageRoot(config map[string]any, baseDir string) string {
	for _, key := range storagePathKeys {
		value := strings.TrimSpace(pickString(config, key))
		if value == "" {
			continue
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(baseDir, value)
		}
		resolved, err := filepath.Abs(value)
		if err != nil {
			continue
		}
		return resolved
	}
	return ""
}

// resolveEmbeddingConfig 只有在配置完整时才启用嵌入，避免半配置状态误触发远程调用。
func resolveEmbeddingConfig(config map[string]any) *EmbeddingConfig {
	section := findEmbeddingSection(config)
	baseURL := strings.TrimRight(pickStrings(section, baseURLKeys), "/")
	apiKey := pickStrings(section, apiKeyKeys)
	model := pickStrings(section, modelKeys)
	if baseURL == "" || apiKey == "" || model == "" {
		return nil
	}
	timeout := pickFloat(section, timeoutKeys, 30)
	if timeout < 1 {
		timeout = 1
	}
	return &EmbeddingConfig{BaseURL: baseURL, APIKey: apiKey, Model: model, TimeoutSeconds: timeout}
}

// findEmbeddingSection 优先读取嵌套配置，必要时兼容平铺字段减少升级摩擦。
func findEmbeddingSection(config map[string]any) map[string]any {
	for _, key := range embeddingKeys {
		if section, ok := config[key].(map[string]any); ok {
			return section
		}
	}
	return config
}

// pickString 提供单字段读取，避免外部直接做不安全的类型断言。
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

// pickStrings 从候选字段里拿第一个非空值，兼容历史命名差异。
func pickStrings(payload map[string]any, keys []string) string {
	for _, key := range keys {
		if value := pickString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

// pickFloat 宽松解析数值，避免配置类型变化直接让功能失效。
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
		case int:
			return float64(typed)
		case string:
			parsed := strings.TrimSpace(typed)
			if parsed == "" {
				continue
			}
			var out float64
			if _, err := fmtSscanf(parsed, &out); err == nil {
				return out
			}
		}
	}
	return fallback
}

// userHomeDir 集中获取 home 目录，避免默认路径在多个文件散落拼装。
func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

// isDir 用于路径决策时快速判断目录存在，避免把普通文件当作记忆根目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
