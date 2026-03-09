package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

const embeddingModelMetaKey = "embedding_model"

var (
	timestampRegexp = regexp.MustCompile(`^(\d{14})`)
	headingRegexp   = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
)

// Service 封装记忆相关核心业务，避免 HTTP 层直接感知数据库与嵌入细节。
type Service struct {
	config   config.AppConfig
	provider EmbeddingProvider
}

// NewService 构造记忆服务，确保搜索、写入和重建共享同一套配置和 provider。
func NewService(cfg config.AppConfig) *Service {
	return &Service{config: cfg, provider: NewEmbeddingProvider(cfg.EmbeddingConfig)}
}

// Search 执行记忆检索并返回结构化结果，统一仅按项目名隔离单库中的不同项目数据。
func (s *Service) Search(ctx context.Context, projectName string, queries []string, debug bool) (SearchResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	effectiveProjectName := normalizeProjectName(projectName)
	var debugCommands []string
	var debugCommandsRef *[]string
	if debug {
		debugCommands = []string{}
		debugCommandsRef = &debugCommands
	}
	matcher := buildQueryMatcher(queries)
	errorKeywordHits, err := s.collectHits(store, "error", effectiveProjectName, queries, matcher, debugCommandsRef)
	if err != nil {
		return SearchResult{}, err
	}
	summaryKeywordHits, err := s.collectHits(store, "summary", effectiveProjectName, queries, matcher, debugCommandsRef)
	if err != nil {
		return SearchResult{}, err
	}
	errorSemanticHits, err := s.collectSemanticHits(store, "error", effectiveProjectName, queries)
	if err != nil {
		return SearchResult{}, err
	}
	summarySemanticHits, err := s.collectSemanticHits(store, "summary", effectiveProjectName, queries)
	if err != nil {
		return SearchResult{}, err
	}
	result := SearchResult{
		Query:         strings.Join(queries, ", "),
		ProjectName:   effectiveProjectName,
		DebugCommands: debugCommands,
		ErrorHits:     s.limitSearchHits(mergeHits(errorKeywordHits, errorSemanticHits), s.lowConfidenceErrorHitLimit()),
		SummaryHits:   s.limitSearchHits(mergeHits(summaryKeywordHits, summarySemanticHits), s.lowConfidenceSummaryHitLimit()),
	}
	return result, nil
}

// Write 写入总结或错误记忆，并把写入人 userid 一并落库以便后续追溯来源。
func (s *Service) Write(ctx context.Context, projectName, gitBranch, userID string, items []api.MemoryWriteItem) (string, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	timestampSeed := now.Unix()
	effectiveProjectName := normalizeProjectName(projectName)
	normalizedGitBranch := normalizeGitBranch(gitBranch)
	rows := make([]Row, 0, len(items))
	memories := make([]models.Memory, 0, len(items))
	for idx, item := range items {
		itemTime := time.Unix(timestampSeed+int64(idx), 0).UTC()
		memories = append(memories, models.Memory{
			UserID:      strings.TrimSpace(userID),
			ProjectName: effectiveProjectName,
			GitBranch:   normalizedGitBranch,
			Type:        strings.TrimSpace(item.Type),
			Title:       sanitizeTitle(item.Title),
			Tags:        models.EncodeTags(item.Tags),
			Summary:     strings.TrimSpace(item.Summary),
			Content:     normalizeMemoryContent(item.Context),
			Timestamp:   itemTime.Format("20060102150405"),
			CreatedAt:   itemTime.Format(time.RFC3339Nano),
		})
	}
	err = store.WithTx(func(txStore *models.Store) error {
		created, err := txStore.CreateMemories(memories)
		if err != nil {
			return err
		}
		rows = make([]Row, 0, len(created))
		for _, item := range created {
			rows = append(rows, memoryRowFromModel(item))
		}
		if !s.provider.Enabled() || len(rows) == 0 {
			return nil
		}
		texts := make([]string, 0, len(rows))
		for _, row := range rows {
			texts = append(texts, BuildMemoryEmbeddingText(row))
		}
		log.Printf("开始为 %d 条记忆生成向量...", len(texts))
		vectors, err := s.provider.EmbedTexts(texts)
		if err != nil {
			return err
		}
		updatedAt := now.Format(time.RFC3339Nano)
		embeddings := make([]models.MemoryEmbedding, 0, len(rows))
		for idx, row := range rows {
			if idx >= len(vectors) {
				break
			}
			embeddings = append(embeddings, models.MemoryEmbedding{MemoryID: row.ID, ProjectName: effectiveProjectName, Type: row.Type, Vector: models.EncodeVector(vectors[idx]), Timestamp: row.Timestamp, UpdatedAt: updatedAt})
		}
		if err := txStore.UpsertMemoryEmbeddings(embeddings); err != nil {
			return err
		}
		return txStore.SetMemoryMetadata(embeddingModelMetaKey, s.provider.ModelName(), updatedAt)
	})
	if err != nil {
		return "", err
	}
	return store.SourceLabel(), nil
}

// EnsureEmbeddingsReady 在服务启动阶段校验模型一致性，避免请求到来后才暴露旧向量问题。
func (s *Service) EnsureEmbeddingsReady(ctx context.Context) (RebuildResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return RebuildResult{}, err
	}
	location := ResolveLocation(s.config)
	return s.rebuildEmbeddingsWithDB(location, store, false)
}

// rebuildEmbeddingsWithDB 在模型变化时全量重建向量，避免新旧维度混用。
func (s *Service) rebuildEmbeddingsWithDB(location Location, store *models.Store, force bool) (RebuildResult, error) {
	if !s.provider.Enabled() {
		return RebuildResult{Changed: false, Message: "未配置嵌入模型，跳过重建。"}, nil
	}
	currentModel, err := store.GetMemoryMetadata(embeddingModelMetaKey)
	if err != nil {
		return RebuildResult{}, err
	}
	storedRows, err := store.ListAllMemories()
	if err != nil {
		return RebuildResult{}, err
	}
	rows := make([]Row, 0, len(storedRows))
	for _, item := range storedRows {
		rows = append(rows, memoryRowFromModel(item))
	}
	embeddingCount, err := store.CountMemoryEmbeddings()
	if err != nil {
		return RebuildResult{}, err
	}
	missingSearchFieldCount, err := store.CountMemoryEmbeddingsMissingSearchFields()
	if err != nil {
		return RebuildResult{}, err
	}
	if currentModel == s.provider.ModelName() && embeddingCount == int64(len(rows)) && missingSearchFieldCount == 0 && !force {
		return RebuildResult{Changed: false, Message: fmt.Sprintf("嵌入模型未变化，继续使用 %s。", s.provider.ModelName())}, nil
	}
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, BuildMemoryEmbeddingText(row))
	}
	log.Printf("开始为 %d 条记忆生成向量...", len(texts))
	vectors, err := s.provider.EmbedTexts(texts)
	if err != nil {
		return RebuildResult{}, err
	}
	log.Printf("向量生成完成，开始写入数据库...")
	rebuiltAt := time.Now().UTC().Format(time.RFC3339Nano)
	total := len(rows)
	err = store.WithTx(func(txStore *models.Store) error {
		if err := txStore.DeleteAllMemoryEmbeddings(); err != nil {
			return err
		}
		embeddings := make([]models.MemoryEmbedding, 0, len(rows))
		for idx, row := range rows {
			if idx >= len(vectors) {
				break
			}
			embeddings = append(embeddings, models.MemoryEmbedding{MemoryID: row.ID, ProjectName: row.ProjectName, Type: row.Type, Vector: models.EncodeVector(vectors[idx]), Timestamp: row.Timestamp, UpdatedAt: rebuiltAt})
			if (idx+1)%100 == 0 || idx+1 == total {
				log.Printf("索引重建进度: %d/%d (%.1f%%)", idx+1, total, float64(idx+1)*100/float64(total))
			}
		}
		if err := txStore.UpsertMemoryEmbeddings(embeddings); err != nil {
			return err
		}
		return txStore.SetMemoryMetadata(embeddingModelMetaKey, s.provider.ModelName(), rebuiltAt)
	})
	if err != nil {
		return RebuildResult{}, err
	}
	_ = location
	return RebuildResult{Changed: true, Message: fmt.Sprintf("已使用模型 %s 重建 %d 条向量。", s.provider.ModelName(), len(vectors))}, nil
}

// collectHits 在数据库记录中筛选关键字命中，并按项目名隔离单库里的不同项目数据。
func (s *Service) collectHits(store *models.Store, source string, projectName string, queries []string, matcher lineMatcher, debugCommands *[]string) ([]Hit, error) {
	if debugCommands != nil {
		*debugCommands = append(*debugCommands, fmt.Sprintf("%s scan: %s [%s/%s]", store.Driver(), store.SourceLabel(), projectName, source))
	}
	storedRows, err := store.SearchMemoriesByProjectAndType(projectName, source, queries)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(storedRows))
	for _, item := range storedRows {
		rows = append(rows, memoryRowFromModel(item))
	}
	now := time.Now().UTC()
	hits := make([]Hit, 0)
	for _, row := range rows {
		lines := splitLines(row.Content)
		lineNumbers := matchLineNumbers(lines, matcher)
		metadataMatched := matchRowMetadata(row, matcher)
		if len(lineNumbers) == 0 && !metadataMatched {
			continue
		}
		ts := parseTimestamp(row.Timestamp)
		hit := Hit{
			ID:         row.ID,
			Source:     source,
			GitBranch:  row.GitBranch,
			Title:      row.Title,
			Tags:       models.DecodeTags(row.Tags),
			Timestamp:  ts,
			Confidence: confidenceByAge(ts, now),
		}
		if metadataMatched && len(lineNumbers) == 0 {
			hit.FileContent = strings.TrimSpace(row.Content)
		} else if len(lineNumbers) > 0 {
			hit.Snippets = buildBodySectionSnippets(lines, lineNumbers, matcher)
		}
		if len(hit.Snippets) == 0 && hit.FileContent == "" {
			hit.FileContent = strings.TrimSpace(row.Content)
		}
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Timestamp.After(hits[j].Timestamp) })
	return hits, nil
}

// collectSemanticHits 在关键字检索之外补充语义召回，并继续按项目名隔离结果。
func (s *Service) collectSemanticHits(store *models.Store, source string, projectName string, queries []string) ([]Hit, error) {
	if !s.provider.Enabled() {
		return nil, nil
	}
	queryTexts := normalizeEmbeddingQueries(queries)
	if len(queryTexts) == 0 {
		return nil, nil
	}
	vectors, err := s.provider.EmbedTexts(queryTexts)
	if err != nil || len(vectors) == 0 {
		return nil, err
	}
	now := time.Now().UTC()
	candidates, err := s.collectSemanticCandidates(store, source, projectName, vectors, now)
	if err != nil {
		return nil, err
	}
	hits, err := s.buildSemanticHits(store, source, projectName, candidates)
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Confidence == hits[j].Confidence {
			return hits[i].Timestamp.After(hits[j].Timestamp)
		}
		return hits[i].Confidence > hits[j].Confidence
	})
	return hits, nil
}

type semanticCandidate struct {
	MemoryID    int64
	Timestamp   time.Time
	Confidence  float64
	SemanticRaw float64
}

// collectSemanticCandidates 先从向量表分页读取候选并打分，避免语义搜索阶段提前全量搬运正文。
func (s *Service) collectSemanticCandidates(store *models.Store, source string, projectName string, vectors [][]float64, now time.Time) ([]semanticCandidate, error) {
	offset := 0
	processed := 0
	hitFetchLimit := s.semanticHitFetchLimit()
	best := make([]semanticCandidate, 0, hitFetchLimit)
	maxCandidateCount := s.semanticCandidateMaxCount()
	for processed < maxCandidateCount {
		remaining := maxCandidateCount - processed
		batchSize := minInt(s.semanticCandidateBatchSize(), remaining)
		items, err := store.ListSemanticEmbeddingCandidates(projectName, source, batchSize, offset)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			vector := models.DecodeVector(item.Vector)
			if len(vector) == 0 {
				continue
			}
			semanticScore := maxSemanticSimilarity(vectors, vector)
			if semanticScore <= s.semanticSimilarityThreshold() {
				continue
			}
			ts := parseTimestamp(item.Timestamp)
			ageScore := confidenceByAge(ts, now)
			candidate := semanticCandidate{
				MemoryID:    item.MemoryID,
				Timestamp:   ts,
				SemanticRaw: semanticScore,
				Confidence:  math.Max(semanticScore, ageScore*0.5+semanticScore*0.5),
			}
			best = appendSemanticCandidate(best, candidate, hitFetchLimit)
		}
		processed += len(items)
		offset += len(items)
		if len(items) < batchSize {
			break
		}
	}
	return best, nil
}

// buildSemanticHits 仅对高分候选回表读取正文，减少非命中记录在语义搜索中的 IO 和内存占用。
func (s *Service) buildSemanticHits(store *models.Store, source string, projectName string, candidates []semanticCandidate) ([]Hit, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	memoryIDs := make([]int64, 0, len(candidates))
	candidateByID := make(map[int64]semanticCandidate, len(candidates))
	for _, item := range candidates {
		memoryIDs = append(memoryIDs, item.MemoryID)
		candidateByID[item.MemoryID] = item
	}
	memoryItems, err := store.ListMemoryLitesByProjectTypeAndIDs(projectName, source, memoryIDs)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(memoryItems))
	for _, item := range memoryItems {
		candidate, ok := candidateByID[item.ID]
		if !ok {
			continue
		}
		hits = append(hits, Hit{
			ID:          item.ID,
			Source:      source,
			GitBranch:   item.GitBranch,
			Title:       item.Title,
			Tags:        models.DecodeTags(item.Tags),
			Timestamp:   candidate.Timestamp,
			Confidence:  candidate.Confidence,
			FileContent: strings.TrimSpace(item.Content),
		})
	}
	return hits, nil
}

// normalizeEmbeddingQueries 统一裁剪查询词，确保嵌入请求只包含用户真实输入。
func normalizeEmbeddingQueries(queries []string) []string {
	out := make([]string, 0, len(queries))
	for _, query := range queries {
		if cleaned := strings.TrimSpace(query); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

// maxSemanticSimilarity 使用多查询向量中的最高分，避免任一查询词被拼接模板稀释。
func maxSemanticSimilarity(queryVectors [][]float64, target []float64) float64 {
	best := 0.0
	for _, vector := range queryVectors {
		if score := CosineSimilarity(vector, target); score > best {
			best = score
		}
	}
	return best
}

// appendSemanticCandidate 只保留最高分候选，避免候选数量持续增长挤占搜索时内存。
func appendSemanticCandidate(items []semanticCandidate, candidate semanticCandidate, limit int) []semanticCandidate {
	items = append(items, candidate)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Confidence == items[j].Confidence {
			return items[i].Timestamp.After(items[j].Timestamp)
		}
		return items[i].Confidence > items[j].Confidence
	})
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

// limitSearchHits 对满分命中不做裁剪，其余命中按分类上限保留，避免强匹配结果被返回上限误伤。
func (s *Service) limitSearchHits(hits []Hit, lowConfidenceLimit int) []Hit {
	if len(hits) == 0 {
		return nil
	}
	limited := make([]Hit, 0, len(hits))
	lowConfidenceCount := 0
	for _, hit := range hits {
		if isPerfectConfidence(hit.Confidence) {
			limited = append(limited, hit)
			continue
		}
		if lowConfidenceCount >= lowConfidenceLimit {
			continue
		}
		limited = append(limited, hit)
		lowConfidenceCount++
	}
	return limited
}

// isPerfectConfidence 把满分命中单独识别出来，避免浮点误差导致精确命中被错误裁剪。
func isPerfectConfidence(value float64) bool {
	return value >= 1-1e-9
}

// lowConfidenceErrorHitLimit 返回错误记忆的低置信度上限，避免错误结果列表被弱相关记忆淹没。
func (s *Service) lowConfidenceErrorHitLimit() int {
	if s.config.SearchConfig == nil || s.config.SearchConfig.LowConfidenceErrorHitLimit < 0 {
		return 10
	}
	return s.config.SearchConfig.LowConfidenceErrorHitLimit
}

// lowConfidenceSummaryHitLimit 返回总结记忆的低置信度上限，兼顾回忆广度与结果可读性。
func (s *Service) lowConfidenceSummaryHitLimit() int {
	if s.config.SearchConfig == nil || s.config.SearchConfig.LowConfidenceSummaryHitLimit < 0 {
		return 10
	}
	return s.config.SearchConfig.LowConfidenceSummaryHitLimit
}

// semanticSimilarityThreshold 返回语义召回阈值，允许通过配置在召回率与精度之间做权衡。
func (s *Service) semanticSimilarityThreshold() float64 {
	if s.config.EmbeddingConfig == nil || s.config.EmbeddingConfig.SemanticSimilarityThreshold <= 0 {
		return 0.15
	}
	return s.config.EmbeddingConfig.SemanticSimilarityThreshold
}

// semanticCandidateBatchSize 返回单批候选量，避免每轮查询读取过多向量造成瞬时内存抖动。
func (s *Service) semanticCandidateBatchSize() int {
	if s.config.EmbeddingConfig == nil || s.config.EmbeddingConfig.SemanticCandidateBatchSize < 1 {
		return 256
	}
	return s.config.EmbeddingConfig.SemanticCandidateBatchSize
}

// semanticCandidateMaxCount 返回最大候选窗口，限制单次语义搜索扫描的向量数量上界。
func (s *Service) semanticCandidateMaxCount() int {
	batchSize := s.semanticCandidateBatchSize()
	if s.config.EmbeddingConfig == nil || s.config.EmbeddingConfig.SemanticCandidateMaxCount < batchSize {
		return maxInt(1024, batchSize)
	}
	return s.config.EmbeddingConfig.SemanticCandidateMaxCount
}

// semanticHitFetchLimit 返回最终允许回表的候选上限，避免高分候选过多时再次拉大正文开销。
func (s *Service) semanticHitFetchLimit() int {
	maxCount := s.semanticCandidateMaxCount()
	if s.config.EmbeddingConfig == nil || s.config.EmbeddingConfig.SemanticHitFetchLimit < 1 {
		return minInt(64, maxCount)
	}
	return minInt(s.config.EmbeddingConfig.SemanticHitFetchLimit, maxCount)
}

// memoryRowFromModel 收敛模型层到业务层的数据映射，避免搜索逻辑直接依赖 GORM 结构体。
func memoryRowFromModel(item models.Memory) Row {
	return Row{
		ID:          item.ID,
		UserID:      item.UserID,
		ProjectName: item.ProjectName,
		GitBranch:   item.GitBranch,
		Type:        item.Type,
		Title:       item.Title,
		Tags:        item.Tags,
		Summary:     item.Summary,
		Content:     item.Content,
		Timestamp:   item.Timestamp,
		CreatedAt:   item.CreatedAt,
	}
}

// mergeHits 关键字命中优先保留原片段展示，再补上语义召回缺失的结果。
func mergeHits(primaryHits, semanticHits []Hit) []Hit {
	merged := make([]Hit, 0, len(primaryHits)+len(semanticHits))
	seen := map[int64]struct{}{}
	semanticByID := map[int64]Hit{}
	for _, hit := range semanticHits {
		semanticByID[hit.ID] = hit
	}
	for _, hit := range primaryHits {
		if semanticHit, ok := semanticByID[hit.ID]; ok && semanticHit.Confidence > hit.Confidence {
			hit.Confidence = semanticHit.Confidence
			if hit.FileContent == "" {
				hit.FileContent = semanticHit.FileContent
			}
		}
		merged = append(merged, hit)
		seen[hit.ID] = struct{}{}
	}
	for _, hit := range semanticHits {
		if _, ok := seen[hit.ID]; ok {
			continue
		}
		merged = append(merged, hit)
	}
	return merged
}

type lineMatcher func(string) bool

// buildQueryMatcher 同时支持正则和大小写不敏感子串匹配，降低查询书写负担。
func buildQueryMatcher(queries []string) lineMatcher {
	patterns := []*regexp.Regexp{}
	fallback := []string{}
	for _, query := range queries {
		trimmed := strings.TrimSpace(query)
		if trimmed == "" {
			continue
		}
		pattern, err := regexp.Compile("(?i)" + trimmed)
		if err == nil {
			patterns = append(patterns, pattern)
			continue
		}
		fallback = append(fallback, strings.ToLower(trimmed))
	}
	return func(text string) bool {
		for _, pattern := range patterns {
			if pattern.MatchString(text) {
				return true
			}
		}
		lowered := strings.ToLower(text)
		for _, item := range fallback {
			if strings.Contains(lowered, item) {
				return true
			}
		}
		return false
	}
}

// parseTimestamp 优先使用数据库里的时间戳，保证排序和展示一致。
func parseTimestamp(timestamp string) time.Time {
	match := timestampRegexp.FindStringSubmatch(strings.TrimSpace(timestamp))
	if len(match) == 2 {
		parsed, err := time.ParseInLocation("20060102150405", match[1], time.UTC)
		if err == nil {
			return parsed
		}
	}
	return time.Now().UTC()
}

// confidenceByAge 保留时间衰减逻辑，让旧记忆自然降权而不是直接丢弃。
func confidenceByAge(ts, now time.Time) float64 {
	ageDays := math.Max(0, now.Sub(ts).Hours()/24)
	return math.Pow(0.5, ageDays/30)
}

// normalizeMemoryContent 统一正文落库规则，避免为兼容旧头部格式继续引入额外解析成本。
func normalizeMemoryContent(content string) string {
	body := strings.TrimSpace(content)
	if body == "" {
		return "## Details\n\n暂无内容。"
	}
	return body
}

// sanitizeTitle 收敛标题字符集，避免持久化标题混入异常字符影响展示与检索。
func sanitizeTitle(title string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(title)), "-")
	cleaned := regexp.MustCompile(`[^0-9A-Za-z_\-\p{Han}]`).ReplaceAllString(trimmed, "")
	if cleaned == "" {
		return "memory"
	}
	runes := []rune(cleaned)
	if len(runes) > 48 {
		return string(runes[:48])
	}
	return cleaned
}

// renderResultMarkdown 输出最终 Markdown，兼容现有 skill 的结果消费方式。
func renderResultMarkdown(query, projectName string, errorHits, summaryHits []Hit, debugCommands []string) string {
	lines := []string{"# Hive Search Result", "- query: " + query, "- project_name: " + projectName}
	if len(debugCommands) > 0 {
		lines = append(lines, "- debug: true", "", "## Debug Commands")
		for _, cmd := range debugCommands {
			lines = append(lines, "- `"+cmd+"`")
		}
	}
	lines = append(lines, "", renderHitsSection("Error Hits", errorHits), "", renderHitsSection("Summary Hits", summaryHits))
	return strings.Join(lines, "\n") + "\n"
}

// Markdown 让结构化搜索结果继续输出兼容旧脚本的 Markdown 文本。
func (r SearchResult) Markdown() string {
	return renderResultMarkdown(r.Query, r.ProjectName, r.ErrorHits, r.SummaryHits, r.DebugCommands)
}

// renderHitsSection 统一渲染分类结果，让空结果也显式可见避免歧义。
func renderHitsSection(title string, hits []Hit) string {
	lines := []string{fmt.Sprintf("## %s (%d)", title, len(hits))}
	if len(hits) == 0 {
		return strings.Join(append(lines, "- (none)"), "\n")
	}
	for idx, hit := range hits {
		lines = append(lines, renderHitMarkdown(hit, idx+1))
		if idx < len(hits)-1 {
			lines = append(lines, "---")
		}
	}
	return strings.Join(lines, "\n")
}

// renderHitMarkdown 渲染单条命中记录，保持 CLI 输出稳定且易于扫描。
func renderHitMarkdown(hit Hit, index int) string {
	lines := []string{fmt.Sprintf("### Record %d", index), "- source: " + hit.Source}
	if hit.GitBranch != "" {
		lines = append(lines, "- git_branch: "+hit.GitBranch)
	}
	if hit.Title != "" {
		lines = append(lines, "- title: "+hit.Title)
	}
	if len(hit.Tags) > 0 {
		lines = append(lines, "- tags: "+strings.Join(hit.Tags, ", "))
	}
	lines = append(lines, "- timestamp: "+hit.Timestamp.Format(time.RFC3339), fmt.Sprintf("- confidence: %.3f", hit.Confidence))
	if hit.FileContent != "" {
		fence := markdownFenceFor(hit.FileContent)
		lines = append(lines, "- file_content:", "  "+fence+"markdown")
		for _, line := range splitLines(hit.FileContent) {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "  "+fence)
	}
	if len(hit.Snippets) > 0 {
		lines = append(lines, "- snippets:")
		for _, snippet := range hit.Snippets {
			fence := markdownFenceFor(snippet.Content)
			lines = append(lines, fmt.Sprintf("  - line_range: %d-%d", snippet.Start, snippet.End), "    content:", "    "+fence+"markdown")
			for _, line := range splitLines(snippet.Content) {
				lines = append(lines, "    "+line)
			}
			lines = append(lines, "    "+fence)
		}
	}
	return strings.Join(lines, "\n")
}

// markdownFenceFor 根据正文内容选择围栏长度，避免嵌套代码块被截断。
func markdownFenceFor(text string) string {
	if strings.Contains(text, "```") {
		return "````"
	}
	return "```"
}

// splitLines 统一处理换行，避免不同平台下行号计算漂移。
func splitLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// matchLineNumbers 按行匹配全文，继续复用原有片段截取逻辑。
func matchLineNumbers(lines []string, matcher lineMatcher) []int {
	out := []int{}
	for idx, line := range lines {
		if matcher(line) {
			out = append(out, idx+1)
		}
	}
	return out
}

// normalizeProjectName 统一裁剪项目名，避免空值污染单库隔离维度。
func normalizeProjectName(projectName string) string {
	cleaned := strings.Trim(strings.TrimSpace(projectName), "./")
	if cleaned == "" {
		return "default-project"
	}
	return cleaned
}

// normalizeGitBranch 统一裁剪分支名，避免头部记录被无意义空白污染。
func normalizeGitBranch(gitBranch string) string {
	return strings.TrimSpace(gitBranch)
}

// isHeadingLine 识别 Markdown 标题，便于按章节返回更完整上下文。
func isHeadingLine(line string) bool {
	return headingRegexp.MatchString(strings.TrimSpace(line))
}

// headingLevel 读取标题层级，保证章节截取不会跨越同级块边界。
func headingLevel(line string) int {
	match := headingRegexp.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) < 2 {
		return 0
	}
	return len(match[1])
}

// headingText 提取标题文本，用于判断是否为标题自身命中。
func headingText(line string) string {
	match := headingRegexp.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) < 3 {
		return ""
	}
	return strings.TrimSpace(match[2])
}

// matchRowMetadata 直接用结构化字段完成元信息匹配，避免再从正文反向恢复文件头。
func matchRowMetadata(row Row, matcher lineMatcher) bool {
	if matcher(row.ProjectName) || matcher(row.GitBranch) || matcher(row.Title) || matcher(row.Summary) {
		return true
	}
	for _, tag := range models.DecodeTags(row.Tags) {
		if matcher(tag) {
			return true
		}
	}
	return false
}

// buildBodySectionSnippets 正文优先按章节返回片段，让结论和约束一起出现。
func buildBodySectionSnippets(lines []string, matchLines []int, matcher lineMatcher) []Snippet {
	if len(lines) == 0 {
		return nil
	}
	snippets := []Snippet{}
	seen := map[string]struct{}{}
	for _, lineNo := range matchLines {
		idx := lineNo - 1
		if idx < 0 || idx >= len(lines) {
			continue
		}
		content := ""
		startIdx := 0
		endIdx := 0
		if isHeadingLine(lines[idx]) && matcher(headingText(lines[idx])) {
			content, startIdx, endIdx = extractSection(lines, idx)
		} else {
			parent := findParentHeadingIndex(lines, idx)
			if parent >= 0 {
				content, startIdx, endIdx = extractSection(lines, parent)
			} else {
				content, startIdx, endIdx = buildBodySnippet(lines, lineNo)
			}
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		absStart := startIdx + 1
		absEnd := endIdx
		key := fmt.Sprintf("%d-%d", absStart, absEnd)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		snippets = append(snippets, Snippet{Start: absStart, End: absEnd, Content: content})
	}
	return snippets
}

// sectionEndIndex 找到当前标题块的结束位置，避免截取过多无关内容。
func sectionEndIndex(bodyLines []string, headingIdx int) int {
	startLevel := headingLevel(bodyLines[headingIdx])
	for idx := headingIdx + 1; idx < len(bodyLines); idx++ {
		if isHeadingLine(bodyLines[idx]) && headingLevel(bodyLines[idx]) <= startLevel {
			return idx
		}
	}
	return len(bodyLines)
}

// extractSection 按标题提取完整章节，提升命中结果的可读性。
func extractSection(bodyLines []string, headingIdx int) (string, int, int) {
	endIdx := sectionEndIndex(bodyLines, headingIdx)
	return strings.TrimSpace(strings.Join(bodyLines[headingIdx:endIdx], "\n")), headingIdx, endIdx
}

// findParentHeadingIndex 在正文命中非标题行时回溯最近标题，保证语义上下文完整。
func findParentHeadingIndex(bodyLines []string, lineIdx int) int {
	for idx := lineIdx; idx >= 0; idx-- {
		if isHeadingLine(bodyLines[idx]) {
			return idx
		}
	}
	return -1
}

// buildBodySnippet 无标题上下文时退化为窗口截取，至少保留附近语义。
func buildBodySnippet(lines []string, matchLine int) (string, int, int) {
	matchIdx := matchLine - 1
	if matchIdx < 0 || matchIdx >= len(lines) {
		return "", 0, 0
	}
	before := minInt(10, matchIdx)
	after := minInt(9, len(lines)-matchIdx-1)
	total := before + 1 + after
	missing := 20 - total
	if missing > 0 {
		extraAfter := minInt(missing, len(lines)-matchIdx-1-after)
		after += extraAfter
		missing -= extraAfter
	}
	if missing > 0 {
		before += minInt(missing, matchIdx-before)
	}
	start := matchIdx - before
	end := matchIdx + after + 1
	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), start, end
}

// parseQueries 兼容 JSON 数组和多参数写法，避免调用方因格式不同而失败。
func ParseQueries(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) == 1 {
		text := strings.TrimSpace(raw[0])
		if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			var payload []any
			if err := json.Unmarshal([]byte(text), &payload); err == nil {
				out := []string{}
				for _, item := range payload {
					if query := strings.TrimSpace(fmt.Sprint(item)); query != "" {
						out = append(out, query)
					}
				}
				if len(out) > 0 {
					return out, nil
				}
			}
		}
	}
	out := []string{}
	for _, item := range raw {
		if query := strings.TrimSpace(item); query != "" {
			out = append(out, query)
		}
	}
	return out, nil
}

// minInt 为片段窗口截取提供最小值比较，避免重复写边界分支。
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// maxInt 为候选窗口下限提供最大值比较，避免配置修正逻辑散落多处。
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
