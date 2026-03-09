package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EmbeddingConfig 统一描述嵌入配置，避免不同子命令各自解析字段。
type EmbeddingConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	TimeoutSeconds float64
}

// MemoryLocation 统一描述当前项目对应的记忆目录与检索维度。
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
	storagePathKeys      = []string{"memory_storage_path", "memoryStorePath", "storage_path", "storagePath"}
	embeddingSectionKeys = []string{"embedding", "embeddings"}
	baseURLKeys          = []string{"base_url", "baseUrl", "url", "endpoint"}
	apiKeyKeys           = []string{"api_key", "apiKey"}
	modelKeys            = []string{"model", "embedding_model", "embeddingModel"}
	timeoutKeys          = []string{"timeout_seconds", "timeoutSeconds"}
)

// resolveMemoryLocation 优先项目内目录，再尝试外挂目录，最后回退到本地目录。
func resolveMemoryLocation(projectRoot string) (MemoryLocation, error) {
	resolvedRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return MemoryLocation{}, err
	}
	projectName := sanitizeProjectName(filepath.Base(resolvedRoot))
	configPath, err := defaultConfigPath()
	if err != nil {
		return MemoryLocation{}, err
	}
	config, _ := loadConfig(configPath)
	defaultConfig, err := buildDefaultConfig()
	if err != nil {
		return MemoryLocation{}, err
	}
	if config == nil {
		config = defaultConfig
	}
	embeddingConfig := resolveEmbeddingConfig(config)
	localMemoryRoot := filepath.Join(resolvedRoot, ".memory")

	if isDir(localMemoryRoot) {
		return MemoryLocation{
			ProjectRoot:     resolvedRoot,
			ProjectName:     projectName,
			SearchRoot:      resolvedRoot,
			MemoryRoot:      localMemoryRoot,
			ExternalEnabled: false,
			ConfigPath:      configPath,
			EmbeddingConfig: embeddingConfig,
		}, nil
	}

	storageRoot := resolveStorageRoot(config, filepath.Dir(configPath))
	if storageRoot != "" {
		externalMemoryRoot := filepath.Join(storageRoot, ".memory")
		if ensureDirUsable(externalMemoryRoot) == nil {
			return MemoryLocation{
				ProjectRoot:     resolvedRoot,
				ProjectName:     projectName,
				SearchRoot:      storageRoot,
				MemoryRoot:      externalMemoryRoot,
				ExternalEnabled: true,
				ConfigPath:      configPath,
				EmbeddingConfig: embeddingConfig,
			}, nil
		}
	}

	return MemoryLocation{
		ProjectRoot:     resolvedRoot,
		ProjectName:     projectName,
		SearchRoot:      resolvedRoot,
		MemoryRoot:      localMemoryRoot,
		ExternalEnabled: false,
		ConfigPath:      configPath,
		EmbeddingConfig: embeddingConfig,
	}, nil
}

// defaultConfigPath 统一配置文件路径，减少路径散落导致的行为差异。
func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "memorymanager", "config.json"), nil
}

// buildDefaultConfig 提供默认外挂目录，兼容首次运行场景。
func buildDefaultConfig() (map[string]any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"memory_storage_path": filepath.Join(home, ".local", "share", "memorymanager"),
		"embedding": map[string]any{
			"base_url":        "",
			"api_key":         "",
			"model":           "",
			"timeout_seconds": 30,
		},
	}, nil
}

// loadConfig 读取配置文件，缺失时尽量创建默认配置，但不因失败中断主流程。
func loadConfig(configPath string) (map[string]any, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		defaultConfig, buildErr := buildDefaultConfig()
		if buildErr != nil {
			return nil, buildErr
		}
		if writeDefaultConfig(configPath, defaultConfig) == nil {
			return defaultConfig, nil
		}
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// writeDefaultConfig 仅在首次运行时落默认配置，避免用户必须手写样板。
func writeDefaultConfig(configPath string, config map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, append(content, '\n'), 0o644)
}

// resolveStorageRoot 解析外挂目录并兼容相对路径配置。
func resolveStorageRoot(config map[string]any, baseDir string) string {
	for _, key := range storagePathKeys {
		value := strings.TrimSpace(asString(config[key]))
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "~") {
			home, err := os.UserHomeDir()
			if err == nil {
				value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
			}
		}
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		return filepath.Clean(filepath.Join(baseDir, value))
	}
	return ""
}

// resolveEmbeddingConfig 提取嵌入配置，字段不完整时视为关闭。
func resolveEmbeddingConfig(config map[string]any) *EmbeddingConfig {
	section := findEmbeddingSection(config)
	if section == nil {
		return nil
	}
	baseURL := strings.TrimRight(pickString(section, baseURLKeys), "/")
	apiKey := pickString(section, apiKeyKeys)
	model := pickString(section, modelKeys)
	if baseURL == "" || apiKey == "" || model == "" {
		return nil
	}
	timeout := pickFloat(section, timeoutKeys, 30)
	if timeout < 1 {
		timeout = 1
	}
	return &EmbeddingConfig{
		BaseURL:        baseURL,
		APIKey:         apiKey,
		Model:          model,
		TimeoutSeconds: timeout,
	}
}

// findEmbeddingSection 优先取嵌套配置，必要时兼容平铺字段。
func findEmbeddingSection(config map[string]any) map[string]any {
	for _, key := range embeddingSectionKeys {
		if payload, ok := config[key].(map[string]any); ok {
			return payload
		}
	}
	return config
}

// pickString 从多个候选键中取第一个非空字符串，减少重复解析代码。
func pickString(payload map[string]any, keys []string) string {
	for _, key := range keys {
		value := strings.TrimSpace(asString(payload[key]))
		if value != "" {
			return value
		}
	}
	return ""
}

// pickFloat 对数字和字符串都做兼容解析，避免配置格式差异导致失效。
func pickFloat(payload map[string]any, keys []string, defaultValue float64) float64 {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case float64:
			return value
		case float32:
			return float64(value)
		case int:
			return float64(value)
		case int64:
			return float64(value)
		case json.Number:
			if v, err := value.Float64(); err == nil {
				return v
			}
		case string:
			if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				return v
			}
		}
	}
	return defaultValue
}

// sanitizeProjectName 保证项目名稳定，避免空名污染共享库维度。
func sanitizeProjectName(name string) string {
	cleaned := strings.Trim(strings.TrimSpace(name), "./")
	if cleaned == "" {
		return "default-project"
	}
	return cleaned
}

// ensureDirUsable 尝试创建目录来判断目标位置是否可用，优先避免运行时权限错误。
func ensureDirUsable(path string) error {
	return os.MkdirAll(path, 0o755)
}

// isDir 用显式目录判断来保留项目内 `.memory` 的优先级语义。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// asString 统一把动态值转成字符串，方便做宽松兼容。
func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
