package memory

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
	"strings"
	"time"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

// EmbeddingProvider 抽象嵌入能力，避免服务层直接依赖具体供应商协议。
type EmbeddingProvider interface {
	Enabled() bool
	ModelName() string
	EmbedTexts(texts []string) ([][]float64, error)
}

// DisabledEmbeddingProvider 保留无嵌入模式，确保关键字流程可以独立工作。
type DisabledEmbeddingProvider struct{}

// Enabled 显式声明当前 provider 不可用，方便上层快速短路。
func (DisabledEmbeddingProvider) Enabled() bool { return false }

// ModelName 关闭模式下返回空模型名，避免误写元数据。
func (DisabledEmbeddingProvider) ModelName() string { return "" }

// EmbedTexts 在关闭模式下不发任何远程请求，保证上层调用方式统一。
func (DisabledEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) { return nil, nil }

// OpenAIEmbeddingProvider 复用 OpenAI 兼容协议，降低接入自建服务和兼容服务商的成本。
type OpenAIEmbeddingProvider struct {
	Config *config.EmbeddingConfig
}

// Enabled 只要实例存在就视为启用，具体字段完整性已在配置层保证。
func (p OpenAIEmbeddingProvider) Enabled() bool { return true }

// ModelName 返回当前使用的模型名，为模型切换检测提供稳定依据。
func (p OpenAIEmbeddingProvider) ModelName() string { return p.Config.Model }

// EmbedTexts 批量调用嵌入接口，保证检索和重建走同一条协议链路。
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
	if strings.TrimSpace(p.Config.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+p.Config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: time.Duration(p.Config.TimeoutSeconds * float64(time.Second)), Transport: buildTransport(p.Config.BaseURL)}
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
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("嵌入请求返回了无法解析的 JSON: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("嵌入响应数量与请求数量不一致")
	}
	vectors := make([][]float64, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("嵌入响应缺少 embedding 字段")
		}
		vectors = append(vectors, item.Embedding)
	}
	return vectors, nil
}

// NewEmbeddingProvider 通过工厂函数隐藏实现细节，让服务层只依赖抽象能力。
func NewEmbeddingProvider(cfg *config.EmbeddingConfig) EmbeddingProvider {
	if cfg == nil {
		return DisabledEmbeddingProvider{}
	}
	return OpenAIEmbeddingProvider{Config: cfg}
}

// buildTransport 私网嵌入服务优先直连，避免代理把可用服务误判成网关错误。
func buildTransport(baseURL string) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	parsed, err := url.Parse(baseURL)
	if err == nil && shouldBypassProxy(parsed.Hostname()) {
		transport.Proxy = nil
	}
	return transport
}

// shouldBypassProxy 对本地和私网地址优先直连，减少内网部署环境下的网络干扰。
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
	addrs, err := net.LookupIP(normalized)
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		parsed, ok := netip.AddrFromSlice(addr)
		if ok && (parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsLinkLocalMulticast()) {
			return true
		}
	}
	return false
}

// BuildMemoryEmbeddingText 为记忆构造语义文本，避免直接用原始存储内容引入过多噪声。
func BuildMemoryEmbeddingText(row Row) string {
	tags := formatEmbeddingTags(row.Tags)
	if strings.EqualFold(strings.TrimSpace(row.Type), "error") {
		return joinNonEmpty([]string{
			"记忆类型: 错误记忆",
			"项目: " + strings.TrimSpace(row.ProjectName),
			"分支: " + strings.TrimSpace(row.GitBranch),
			"问题: " + strings.TrimSpace(row.Title),
			"现象摘要: " + strings.TrimSpace(row.Summary),
			"故障标签: " + tags,
			"排障记录:",
			strings.TrimSpace(row.Content),
		})
	}
	return joinNonEmpty([]string{
		"记忆类型: 总结记忆",
		"项目: " + strings.TrimSpace(row.ProjectName),
		"分支: " + strings.TrimSpace(row.GitBranch),
		"主题: " + strings.TrimSpace(row.Title),
		"摘要: " + strings.TrimSpace(row.Summary),
		"标签: " + tags,
		"正文:",
		strings.TrimSpace(row.Content),
	})
}

// CosineSimilarity 用余弦相似度衡量语义接近度，避免向量长度差异带来偏置。
func CosineSimilarity(left, right []float64) float64 {
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

// formatEmbeddingTags 把标签整理成自然文本，减少 JSON 符号给向量引入无意义噪声。
func formatEmbeddingTags(raw string) string {
	return strings.Join(models.DecodeTags(raw), "、")
}

// joinNonEmpty 过滤空占位文本，避免嵌入向量被无意义字段稀释。
func joinNonEmpty(parts []string) string {
	out := []string{}
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" && trimmed != "标签:" && trimmed != "故障标签:" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n")
}
