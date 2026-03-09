package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var frontMatterRegexp = regexp.MustCompile(`\A---\n.*?\n---\n?`)

// EmbeddingProvider 抽象嵌入能力，避免搜索和写入直接耦合具体服务商。
type EmbeddingProvider interface {
	Enabled() bool
	ModelName() string
	EmbedTexts(texts []string) ([][]float64, error)
}

// DisabledEmbeddingProvider 保留无嵌入模式，确保关键字流程可独立工作。
type DisabledEmbeddingProvider struct{}

// Enabled 用显式开关让上层可以快速短路。
func (DisabledEmbeddingProvider) Enabled() bool { return false }

// ModelName 在关闭模式下返回空模型名，避免误写元数据。
func (DisabledEmbeddingProvider) ModelName() string { return "" }

// EmbedTexts 在关闭模式下不发请求，保持调用方代码统一。
func (DisabledEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) { return nil, nil }

// OpenAIEmbeddingProvider 复用 OpenAI 兼容接口，降低接入成本。
type OpenAIEmbeddingProvider struct{ Config *EmbeddingConfig }

// Enabled 只要实例存在就视为可用，具体校验已在配置层完成。
func (p OpenAIEmbeddingProvider) Enabled() bool { return true }

// ModelName 返回当前模型名，供重建逻辑判断是否需要刷新向量。
func (p OpenAIEmbeddingProvider) ModelName() string { return p.Config.Model }

// EmbedTexts 批量调用嵌入接口，保证搜索和重建使用同一协议链路。
func (p OpenAIEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{"model": p.Config.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, p.Config.BaseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: time.Duration(p.Config.TimeoutSeconds * float64(time.Second)), Transport: buildEmbeddingTransport(p.Config.BaseURL)}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("嵌入请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("嵌入请求失败: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payloadBody struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payloadBody); err != nil {
		return nil, fmt.Errorf("嵌入请求返回了无法解析的 JSON: %w", err)
	}
	if len(payloadBody.Data) != len(texts) {
		return nil, fmt.Errorf("嵌入响应数量与请求数量不一致")
	}
	vectors := make([][]float64, 0, len(payloadBody.Data))
	for _, item := range payloadBody.Data {
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("嵌入响应缺少 embedding 字段")
		}
		vectors = append(vectors, item.Embedding)
	}
	return vectors, nil
}

// createEmbeddingProvider 通过工厂函数隐藏实现细节，让调用方只依赖统一接口。
func createEmbeddingProvider(config *EmbeddingConfig) EmbeddingProvider {
	if config == nil {
		return DisabledEmbeddingProvider{}
	}
	return OpenAIEmbeddingProvider{Config: config}
}

// buildEmbeddingTransport 私网嵌入服务优先直连，避免代理把可用服务误判成网关错误。
func buildEmbeddingTransport(baseURL string) *http.Transport {
	hostname := ""
	if parsed, err := url.Parse(baseURL); err == nil {
		hostname = parsed.Hostname()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if shouldBypassProxy(hostname) {
		transport.Proxy = nil
	}
	return transport
}

// shouldBypassProxy 私网和本地地址优先直连，减少本地部署场景误走代理。
func shouldBypassProxy(hostname string) bool {
	normalized := strings.ToLower(strings.TrimSpace(hostname))
	if normalized == "" {
		return false
	}
	if normalized == "localhost" || normalized == "host.docker.internal" {
		return true
	}
	if addr, err := netip.ParseAddr(normalized); err == nil {
		return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast()
	}
	if addrs, err := net.LookupIP(normalized); err == nil {
		for _, addr := range addrs {
			if ip, ok := netip.AddrFromSlice(addr); ok && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
				return true
			}
		}
	}
	return false
}

// formatEmbeddingTags 把标签整理成自然文本，减少 JSON 符号带来的语义噪声。
func formatEmbeddingTags(raw any) string {
	switch value := raw.(type) {
	case []string:
		return strings.Join(value, "、")
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return ""
		}
		if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			return strings.Join(decodeTags(text), "、")
		}
		parts := strings.Split(text, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if item := strings.TrimSpace(part); item != "" {
				out = append(out, item)
			}
		}
		return strings.Join(out, "、")
	default:
		return ""
	}
}

// stripFrontMatter 去掉持久化内容中的 YAML 头部，避免元数据重复稀释正文语义。
func stripFrontMatter(content string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		return ""
	}
	return strings.TrimSpace(frontMatterRegexp.ReplaceAllString(text, ""))
}

// buildSummaryEmbeddingText 为总结记忆组织更偏结论导向的嵌入文本。
func buildSummaryEmbeddingText(row MemoryRow) string {
	parts := []string{
		"记忆类型: 总结记忆",
		"项目: " + strings.TrimSpace(row.ProjectName),
		"主题: " + strings.TrimSpace(row.Title),
		"摘要: " + strings.TrimSpace(row.Summary),
		"标签: " + formatEmbeddingTags(row.Tags),
		"正文:",
		stripFrontMatter(row.Content),
	}
	return joinNonEmpty(parts)
}

// buildErrorEmbeddingText 为错误记忆强调故障现象与修复线索，提升排障召回精度。
func buildErrorEmbeddingText(row MemoryRow) string {
	parts := []string{
		"记忆类型: 错误记忆",
		"项目: " + strings.TrimSpace(row.ProjectName),
		"问题: " + strings.TrimSpace(row.Title),
		"现象摘要: " + strings.TrimSpace(row.Summary),
		"故障标签: " + formatEmbeddingTags(row.Tags),
		"排障记录:",
		stripFrontMatter(row.Content),
	}
	return joinNonEmpty(parts)
}

// buildMemoryEmbeddingText 按记忆类型切换模板，让不同场景都保留最关键语义。
func buildMemoryEmbeddingText(row MemoryRow) string {
	if strings.EqualFold(strings.TrimSpace(row.Type), "error") {
		return buildErrorEmbeddingText(row)
	}
	return buildSummaryEmbeddingText(row)
}

// buildQueryEmbeddingText 为查询构造与目标记忆类型对齐的模板，减少短词语义损耗。
func buildQueryEmbeddingText(source string, queries []string) string {
	cleaned := make([]string, 0, len(queries))
	for _, query := range queries {
		if item := strings.TrimSpace(query); item != "" {
			cleaned = append(cleaned, item)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	joined := strings.Join(cleaned, "、")
	if source == "error" {
		return strings.Join([]string{
			"查询类型: 错误排查记忆检索",
			"关注问题: " + joined,
			"检索目标: 查找相似的错误现象、触发条件、根因和修复结论。",
		}, "\n")
	}
	return strings.Join([]string{
		"查询类型: 总结记忆检索",
		"关注主题: " + joined,
		"检索目标: 查找相关主题、关键结论、约束条件和实现经验。",
	}, "\n")
}

// cosineSimilarity 用余弦相似度衡量语义接近度，避免向量长度差异带来偏置。
func cosineSimilarity(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var numerator float64
	var leftNorm float64
	var rightNorm float64
	for idx := range left {
		numerator += left[idx] * right[idx]
		leftNorm += left[idx] * left[idx]
		rightNorm += right[idx] * right[idx]
	}
	if leftNorm <= 0 || rightNorm <= 0 {
		return 0
	}
	return numerator / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

// joinNonEmpty 过滤空占位文本，减少嵌入噪声。
func joinNonEmpty(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" && trimmed != "标签:" && trimmed != "故障标签:" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n")
}
