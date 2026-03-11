package memory

import (
	"strings"
	"testing"
)

// TestBuildMemoryEmbeddingTextSummaryUsesStructuredTemplate 验证总结记忆会优先抽取统一模板字段，避免长正文淹没关键信息。
func TestBuildMemoryEmbeddingTextSummaryUsesStructuredTemplate(t *testing.T) {
	t.Helper()
	text := BuildMemoryEmbeddingText(Row{
		ProjectName: "payment-service",
		GitBranch:   "feature/summary-template",
		Type:        "summary",
		Title:       "连接池调优",
		Summary:     "记录连接池调优后的稳定结论。",
		Tags:        `["数据库","连接池"]`,
		Content:     "# internal/db/pool.go\n\n## 调优策略\n\n- 详情: 统一复用长连接池并降低突发建连。\n- 结论: 峰值时错误率明显下降。\n- 约束: 需要先确认连接上限。\n- 依赖: internal/config/config.go",
	})
	for _, expected := range []string{
		"标题: 连接池调优",
		"详情: 统一复用长连接池并降低突发建连。",
		"结论: 峰值时错误率明显下降。",
		"约束: 需要先确认连接上限。",
		"依赖: internal/config/config.go",
		"关键章节: internal/db/pool.go；调优策略",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("总结记忆 embedding 文本缺少 %q: %s", expected, text)
		}
	}
}

// TestBuildMemoryEmbeddingTextErrorUsesStructuredTemplate 验证错误记忆会优先保留现象、根因和修复等关键排障字段。
func TestBuildMemoryEmbeddingTextErrorUsesStructuredTemplate(t *testing.T) {
	t.Helper()
	text := BuildMemoryEmbeddingText(Row{
		ProjectName: "payment-service",
		GitBranch:   "feature/error-template",
		Type:        "error",
		Title:       "支付超时排查",
		Summary:     "记录支付超时的根因与修复。",
		Tags:        `["支付","超时"]`,
		Content:     "# internal/pay/retry.go\n\n## 超时故障\n\n- 错误现象: 高峰期大量支付请求超时。\n- 触发条件: 重试队列持续积压。\n- 根因: 下游连接池耗尽。\n- 修复动作: 限制重试并扩容连接池。\n- 修复结论: 请求恢复稳定。\n- 验证结果: 超时率从 12% 降到 0.3%。",
	})
	for _, expected := range []string{
		"标题: 支付超时排查",
		"错误现象: 高峰期大量支付请求超时。",
		"触发条件: 重试队列持续积压。",
		"根因: 下游连接池耗尽。",
		"修复动作: 限制重试并扩容连接池。",
		"修复结论: 请求恢复稳定。",
		"验证结果: 超时率从 12% 降到 0.3%。",
		"关键章节: internal/pay/retry.go；超时故障",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("错误记忆 embedding 文本缺少 %q: %s", expected, text)
		}
	}
}
