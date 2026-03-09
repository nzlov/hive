package memory

import (
	"strconv"
	"strings"
	"time"
)

// searchCache 保存查询向量和语义命中缓存，避免高频查询重复访问嵌入服务与向量检索。
type searchCache struct {
	queryEmbeddings map[string]queryEmbeddingsCacheEntry
	semanticHits    map[string]semanticHitsCacheEntry
}

type queryEmbeddingsCacheEntry struct {
	Vectors   [][]float64
	ExpiresAt time.Time
}

type semanticHitsCacheEntry struct {
	Hits      []Hit
	ExpiresAt time.Time
}

// newSearchCache 初始化缓存容器，避免服务首次查询时再做判空分支。
func newSearchCache() *searchCache {
	return &searchCache{
		queryEmbeddings: map[string]queryEmbeddingsCacheEntry{},
		semanticHits:    map[string]semanticHitsCacheEntry{},
	}
}

// cacheEnabled 判断是否启用缓存，保证缓存能力可以通过配置一键关闭。
func (s *Service) cacheEnabled() bool {
	return s.config.SearchConfig != nil && s.config.SearchConfig.CacheEnabled
}

// cacheQueryEmbeddingTTL 返回查询向量缓存 TTL，避免缓存时长在逻辑代码里写死。
func (s *Service) cacheQueryEmbeddingTTL() time.Duration {
	if s.config.SearchConfig == nil || s.config.SearchConfig.CacheQueryEmbeddingTTL < 1 {
		return 0
	}
	return time.Duration(s.config.SearchConfig.CacheQueryEmbeddingTTL) * time.Second
}

// cacheSemanticHitsTTL 返回语义命中缓存 TTL，避免缓存时长在逻辑代码里写死。
func (s *Service) cacheSemanticHitsTTL() time.Duration {
	if s.config.SearchConfig == nil || s.config.SearchConfig.CacheSemanticHitsTTL < 1 {
		return 0
	}
	return time.Duration(s.config.SearchConfig.CacheSemanticHitsTTL) * time.Second
}

// cacheMaxEntries 返回缓存容量上限，避免缓存无限增长占用内存。
func (s *Service) cacheMaxEntries() int {
	if s.config.SearchConfig == nil || s.config.SearchConfig.CacheMaxEntries < 1 {
		return 1
	}
	return s.config.SearchConfig.CacheMaxEntries
}

// queryEmbeddingsCacheKey 构造查询向量缓存键，避免不同模型和查询串共享同一缓存项。
func (s *Service) queryEmbeddingsCacheKey(queries []string) string {
	return strings.Join([]string{"emb", s.provider.ModelName(), strings.Join(queries, "\x1f")}, "|")
}

// semanticHitsCacheKey 构造语义命中缓存键，确保配置变化后不会复用旧策略结果。
func (s *Service) semanticHitsCacheKey(source, projectName string, queries []string) string {
	return strings.Join([]string{"sem", source, projectName, s.provider.ModelName(), s.semanticSearchConfigVersion(), strings.Join(queries, "\x1f")}, "|")
}

// semanticSearchConfigVersion 生成语义相关配置版本号，避免配置变更后命中过期缓存。
func (s *Service) semanticSearchConfigVersion() string {
	if s.config.EmbeddingConfig == nil {
		return "embedding-disabled"
	}
	return strings.Join([]string{
		s.config.EmbeddingConfig.SemanticWindowMode,
		formatFloatForCache(s.config.EmbeddingConfig.SemanticSimilarityThreshold),
		formatFloatForCache(s.config.EmbeddingConfig.DecayAgeWeight),
		formatFloatForCache(s.config.EmbeddingConfig.DecaySemanticWeight),
		formatFloatForCache(s.config.EmbeddingConfig.DecaySummaryHalfLifeDays),
		formatFloatForCache(s.config.EmbeddingConfig.DecayErrorHalfLifeDays),
	}, ",")
}

// formatFloatForCache 统一浮点格式，避免缓存键在不同平台出现格式差异。
func formatFloatForCache(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// getCachedQueryEmbeddings 读取查询向量缓存，命中后返回副本避免共享底层切片造成串改。
func (s *Service) getCachedQueryEmbeddings(key string, now time.Time) ([][]float64, bool) {
	if !s.cacheEnabled() || s.cache == nil {
		return nil, false
	}
	s.cacheMu.RLock()
	entry, ok := s.cache.queryEmbeddings[key]
	s.cacheMu.RUnlock()
	if !ok || entry.ExpiresAt.Before(now) {
		return nil, false
	}
	out := make([][]float64, 0, len(entry.Vectors))
	for _, vector := range entry.Vectors {
		copied := make([]float64, len(vector))
		copy(copied, vector)
		out = append(out, copied)
	}
	return out, true
}

// setCachedQueryEmbeddings 写入查询向量缓存，避免重复请求嵌入服务。
func (s *Service) setCachedQueryEmbeddings(key string, vectors [][]float64, now time.Time) {
	if !s.cacheEnabled() || s.cache == nil {
		return
	}
	ttl := s.cacheQueryEmbeddingTTL()
	if ttl <= 0 {
		return
	}
	entryVectors := make([][]float64, 0, len(vectors))
	for _, vector := range vectors {
		copied := make([]float64, len(vector))
		copy(copied, vector)
		entryVectors = append(entryVectors, copied)
	}
	s.cacheMu.Lock()
	s.cache.queryEmbeddings[key] = queryEmbeddingsCacheEntry{Vectors: entryVectors, ExpiresAt: now.Add(ttl)}
	s.evictCacheIfNeededLocked()
	s.cacheMu.Unlock()
}

// getCachedSemanticHits 读取语义命中缓存，命中后返回副本避免调用链修改缓存内容。
func (s *Service) getCachedSemanticHits(key string, now time.Time) ([]Hit, bool) {
	if !s.cacheEnabled() || s.cache == nil {
		return nil, false
	}
	s.cacheMu.RLock()
	entry, ok := s.cache.semanticHits[key]
	s.cacheMu.RUnlock()
	if !ok || entry.ExpiresAt.Before(now) {
		return nil, false
	}
	out := make([]Hit, 0, len(entry.Hits))
	for _, hit := range entry.Hits {
		out = append(out, hit)
	}
	return out, true
}

// setCachedSemanticHits 写入语义命中缓存，避免同类查询重复走向量检索。
func (s *Service) setCachedSemanticHits(key string, hits []Hit, now time.Time) {
	if !s.cacheEnabled() || s.cache == nil {
		return
	}
	ttl := s.cacheSemanticHitsTTL()
	if ttl <= 0 {
		return
	}
	entryHits := make([]Hit, 0, len(hits))
	for _, hit := range hits {
		entryHits = append(entryHits, hit)
	}
	s.cacheMu.Lock()
	s.cache.semanticHits[key] = semanticHitsCacheEntry{Hits: entryHits, ExpiresAt: now.Add(ttl)}
	s.evictCacheIfNeededLocked()
	s.cacheMu.Unlock()
}

// invalidateSearchCache 清空检索相关缓存，避免写入或重建后继续返回旧结果。
func (s *Service) invalidateSearchCache() {
	if s.cache == nil {
		return
	}
	s.cacheMu.Lock()
	s.cache.queryEmbeddings = map[string]queryEmbeddingsCacheEntry{}
	s.cache.semanticHits = map[string]semanticHitsCacheEntry{}
	s.cacheMu.Unlock()
}

// evictCacheIfNeededLocked 在持锁状态下执行容量回收，避免缓存容量失控。
func (s *Service) evictCacheIfNeededLocked() {
	maxEntries := s.cacheMaxEntries()
	for len(s.cache.queryEmbeddings) > maxEntries {
		for key := range s.cache.queryEmbeddings {
			delete(s.cache.queryEmbeddings, key)
			break
		}
	}
	for len(s.cache.semanticHits) > maxEntries {
		for key := range s.cache.semanticHits {
			delete(s.cache.semanticHits, key)
			break
		}
	}
}
