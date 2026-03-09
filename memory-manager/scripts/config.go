package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"memory-manager/internal/config"
	"memory-manager/internal/memory"
)

// scriptConfig 收敛脚本运行期依赖，避免每个子命令各自重复读取配置。
type scriptConfig struct {
	ServerBaseURL string
}

// loadScriptConfig 只保留脚本调用服务端所需的基础配置，降低客户端职责。
func loadScriptConfig() (scriptConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return scriptConfig{}, err
	}
	return scriptConfig{ServerBaseURL: cfg.ServerBaseURL}, nil
}

// resolveProjectRoot 统一规范项目根目录，避免不同子命令对相对路径的理解不一致。
func resolveProjectRoot(root string) (string, error) {
	return filepath.Abs(root)
}

// parseScriptQueries 继续复用服务端的查询解析规则，避免客户端和服务端对数组写法理解不一致。
func parseScriptQueries(raw []string) ([]string, error) {
	return memory.ParseQueries(raw)
}

// splitTags 统一清洗逗号分隔标签，避免空标签进入远程请求体。
func splitTags(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// normalizeTags 兼容字符串和数组标签写法，减少批量写入时的额外转换代码。
func normalizeTags(raw any) []string {
	switch typed := raw.(type) {
	case string:
		return splitTags(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringify(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// stringify 宽松转字符串，避免动态 JSON 字段类型差异影响参数兼容性。
func stringify(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
