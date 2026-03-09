package main

import (
	"fmt"
	"regexp"
	"strings"
)

// fmtSscanf 单独封装扫描逻辑，避免工具函数直接依赖实现细节。
func fmtSscanf(input string, out *float64) (int, error) {
	return fmt.Sscanf(input, "%f", out)
}

// sanitizeTitle 收敛标题字符集，避免持久化名称混入异常字符影响展示与检索。
func sanitizeTitle(title string) string {
	title = regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(title), "-")
	title = regexp.MustCompile(`[^0-9A-Za-z_\-\p{Han}]`).ReplaceAllString(title, "")
	if title == "" {
		return "memory"
	}
	if len([]rune(title)) > 48 {
		return string([]rune(title)[:48])
	}
	return title
}

// splitTags 统一清洗标签，避免空标签进入持久化与检索索引。
func splitTags(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// normalizeTags 兼容字符串和数组写法，减少批量写入时的额外转换成本。
func normalizeTags(raw any) []string {
	switch typed := raw.(type) {
	case string:
		return splitTags(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// markdownFenceFor 根据正文选择围栏长度，避免嵌套代码块被意外截断。
func markdownFenceFor(text string) string {
	if strings.Contains(text, "```") {
		return "````"
	}
	return "```"
}
