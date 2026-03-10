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
	"sync"
	"time"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

const embeddingModelMetaKey = "embedding_model"

var (
	timestampRegexp = regexp.MustCompile(`^(\d{14})`)
	headingRegexp   = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	keywordTermRE   = regexp.MustCompile(`[\p{Han}\p{L}\p{N}_]+`)
)

// Service 封装记忆相关核心业务，避免 HTTP 层直接感知数据库与嵌入细节。
type Service struct {
	config   config.AppConfig
	provider EmbeddingProvider
	cacheMu  sync.RWMutex
	cache    *searchCache
}

// NewService 构造记忆服务，确保搜索、写入和重建共享同一套配置和 provider。
func NewService(cfg config.AppConfig) *Service {
	return &Service{config: cfg, provider: NewEmbeddingProvider(cfg.EmbeddingConfig), cache: newSearchCache()}
}

// Search 执行记忆检索并返回结构化结果，统一仅按项目名隔离单库中的不同项目数据。
func (s *Service) Search(ctx context.Context, projectName string, queries []string, debug bool) (SearchResult, error) {
	rawResult, err := s.searchHits(ctx, normalizeProjectName(projectName), queries, debug)
	if err != nil {
		return SearchResult{}, err
	}
	rawResult.ErrorHits = s.limitSearchHits(rawResult.ErrorHits, s.lowConfidenceErrorHitLimit())
	rawResult.SummaryHits = s.limitSearchHits(rawResult.SummaryHits, s.lowConfidenceSummaryHitLimit())
	return rawResult, nil
}

// List 为管理端提供分页列表；无查询词时走时间排序，有查询词时复用搜索逻辑并返回完整命中集。
func (s *Service) List(ctx context.Context, page, pageSize int, queries []string) (MemoryListResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return MemoryListResult{}, err
	}
	page, pageSize = normalizePagination(page, pageSize)
	cleanedQueries := normalizePlainQueries(queries)
	if len(cleanedQueries) == 0 {
		items, total, err := store.ListMemoriesPaginated(page, pageSize, nil)
		if err != nil {
			return MemoryListResult{}, err
		}
		listItems := make([]MemoryListItem, 0, len(items))
		for _, item := range items {
			listItems = append(listItems, MemoryListItem{Memory: item})
		}
		return MemoryListResult{
			Items:     listItems,
			Total:     total,
			Page:      page,
			PageSize:  pageSize,
			TotalPage: computeTotalPages(total, pageSize),
		}, nil
	}
	rawResult, err := s.searchHits(ctx, "", cleanedQueries, false)
	if err != nil {
		return MemoryListResult{}, err
	}
	mergedHits := mergeListHits(rawResult.ErrorHits, rawResult.SummaryHits)
	total := int64(len(mergedHits))
	pagedHits := paginateHits(mergedHits, page, pageSize)
	orderedItems, err := s.buildListItems(store, pagedHits)
	if err != nil {
		return MemoryListResult{}, err
	}
	return MemoryListResult{
		Items:     orderedItems,
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
		TotalPage: computeTotalPages(total, pageSize),
	}, nil
}

// searchHits 收敛关键字与向量召回实现，让搜索接口和管理列表共享同一套命中逻辑。
func (s *Service) searchHits(ctx context.Context, projectName string, queries []string, debug bool) (SearchResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	var debugCommands []string
	var debugCommandsRef *[]string
	if debug {
		debugCommands = []string{}
		debugCommandsRef = &debugCommands
	}
	effectiveQueries := s.keywordQueries(queries)
	matcher := buildQueryMatcher(effectiveQueries)
	errorKeywordHits, err := s.collectHits(store, "error", projectName, queries, matcher, debugCommandsRef)
	if err != nil {
		return SearchResult{}, err
	}
	summaryKeywordHits, err := s.collectHits(store, "summary", projectName, queries, matcher, debugCommandsRef)
	if err != nil {
		return SearchResult{}, err
	}
	errorSemanticHits, err := s.collectSemanticHits(store, "error", projectName, queries)
	if err != nil {
		return SearchResult{}, err
	}
	summarySemanticHits, err := s.collectSemanticHits(store, "summary", projectName, queries)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{
		Query:         strings.Join(queries, ", "),
		ProjectName:   projectName,
		DebugCommands: debugCommands,
		ErrorHits:     s.mergeHits("error", errorKeywordHits, errorSemanticHits),
		SummaryHits:   s.mergeHits("summary", summaryKeywordHits, summarySemanticHits),
	}, nil
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
	s.invalidateSearchCache()
	return store.SourceLabel(), nil
}

// Update 更新单条记忆的可编辑字段，并在成功后同步刷新对应向量与搜索缓存。
func (s *Service) Update(ctx context.Context, id int64, request api.UpdateMemoryRequest) (models.Memory, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return models.Memory{}, err
	}
	var updated models.Memory
	err = store.WithTx(func(txStore *models.Store) error {
		existing, err := txStore.GetMemoryByID(id)
		if err != nil {
			return err
		}
		updated, err = txStore.SaveMemoryEditableFields(models.Memory{
			ID:      existing.ID,
			Title:   sanitizeTitle(request.Title),
			Tags:    models.EncodeTags(request.Tags),
			Summary: strings.TrimSpace(request.Summary),
			Content: normalizeMemoryContent(request.Content),
		})
		if err != nil {
			return err
		}
		if !s.provider.Enabled() {
			return nil
		}
		row := memoryRowFromModel(updated)
		vectors, err := s.provider.EmbedTexts([]string{BuildMemoryEmbeddingText(row)})
		if err != nil {
			return err
		}
		if len(vectors) == 0 {
			return nil
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		if err := txStore.UpsertMemoryEmbeddings([]models.MemoryEmbedding{{
			MemoryID:    updated.ID,
			ProjectName: updated.ProjectName,
			Type:        updated.Type,
			Vector:      models.EncodeVector(vectors[0]),
			Timestamp:   updated.Timestamp,
			UpdatedAt:   updatedAt,
		}}); err != nil {
			return err
		}
		return txStore.SetMemoryMetadata(embeddingModelMetaKey, s.provider.ModelName(), updatedAt)
	})
	if err != nil {
		return models.Memory{}, err
	}
	s.invalidateSearchCache()
	return updated, nil
}

// EnsureEmbeddingsReady 在服务启动阶段校验模型一致性，避免请求到来后才暴露旧向量问题。
func (s *Service) EnsureEmbeddingsReady(ctx context.Context) (RebuildResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return RebuildResult{}, err
	}
	if err := store.EnsureVectorBackendReady(); err != nil {
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
	if currentModel == s.provider.ModelName() && embeddingCount == int64(len(rows)) && !force {
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
	s.invalidateSearchCache()
	_ = location
	return RebuildResult{Changed: true, Message: fmt.Sprintf("已使用模型 %s 重建 %d 条向量。", s.provider.ModelName(), len(vectors))}, nil
}

// collectHits 在数据库记录中筛选关键字命中，并按项目名隔离单库里的不同项目数据。
func (s *Service) collectHits(store *models.Store, source string, projectName string, queries []string, matcher lineMatcher, debugCommands *[]string) ([]Hit, error) {
	if debugCommands != nil {
		*debugCommands = append(*debugCommands, fmt.Sprintf("%s scan: %s [%s/%s]", store.Driver(), store.SourceLabel(), projectName, source))
	}
	effectiveQueries := s.keywordQueries(queries)
	storedRows, err := store.SearchMemoriesByProjectAndType(projectName, source, effectiveQueries)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(storedRows))
	for _, item := range storedRows {
		rows = append(rows, memoryRowFromModel(item))
	}
	keywordScoreByID := map[int64]float64{}
	if s.keywordMode() == "bm25" {
		rows, keywordScoreByID = s.rankRowsByKeywordBM25(rows, effectiveQueries)
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
		keywordConfidence := s.confidenceByAgeForType(source, ts, now)
		if score, ok := keywordScoreByID[row.ID]; ok {
			keywordConfidence = score
		}
		hit := Hit{
			ID:         row.ID,
			Source:     source,
			GitBranch:  row.GitBranch,
			Title:      row.Title,
			Tags:       models.DecodeTags(row.Tags),
			Timestamp:  ts,
			Confidence: keywordConfidence,
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
	if s.keywordMode() != "bm25" {
		sort.Slice(hits, func(i, j int) bool { return hits[i].Timestamp.After(hits[j].Timestamp) })
	}
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
	now := time.Now().UTC()
	semanticCacheKey := s.semanticHitsCacheKey(source, projectName, queryTexts)
	if hits, ok := s.getCachedSemanticHits(semanticCacheKey, now); ok {
		return hits, nil
	}
	queryEmbeddingCacheKey := s.queryEmbeddingsCacheKey(queryTexts)
	vectors, ok := s.getCachedQueryEmbeddings(queryEmbeddingCacheKey, now)
	var err error
	if !ok {
		vectors, err = s.provider.EmbedTexts(queryTexts)
		if err != nil {
			return nil, err
		}
		s.setCachedQueryEmbeddings(queryEmbeddingCacheKey, vectors, now)
	}
	if err != nil || len(vectors) == 0 {
		return nil, err
	}
	hits, err := s.buildSemanticHitsFromBackend(store, source, projectName, vectors)
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Confidence == hits[j].Confidence {
			return hits[i].Timestamp.After(hits[j].Timestamp)
		}
		return hits[i].Confidence > hits[j].Confidence
	})
	s.setCachedSemanticHits(semanticCacheKey, hits, now)
	return hits, nil
}

// buildSemanticHitsFromBackend 使用数据库向量能力完成检索，避免服务层再做全量候选扫描。
func (s *Service) buildSemanticHitsFromBackend(store *models.Store, source string, projectName string, queryVectors [][]float64) ([]Hit, error) {
	hitFetchLimit, err := s.semanticHitFetchLimitForQuery(store, source, projectName)
	if err != nil {
		return nil, err
	}
	bestByID := map[int64]float64{}
	for _, queryVector := range queryVectors {
		items, err := store.SearchMemoryEmbeddingsByVector(projectName, source, queryVector, hitFetchLimit)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Similarity <= s.semanticSimilarityThreshold() {
				continue
			}
			if item.Similarity > bestByID[item.MemoryID] {
				bestByID[item.MemoryID] = item.Similarity
			}
		}
	}
	if len(bestByID) == 0 {
		return nil, nil
	}
	memoryIDs := make([]int64, 0, len(bestByID))
	for memoryID := range bestByID {
		memoryIDs = append(memoryIDs, memoryID)
	}
	memoryItems, err := store.ListMemoryLitesByProjectTypeAndIDs(projectName, source, memoryIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hits := make([]Hit, 0, len(memoryItems))
	for _, item := range memoryItems {
		semanticScore, ok := bestByID[item.ID]
		if !ok {
			continue
		}
		ts := parseTimestamp(item.Timestamp)
		ageScore := s.confidenceByAgeForType(source, ts, now)
		hits = append(hits, Hit{
			ID:          item.ID,
			Source:      source,
			GitBranch:   item.GitBranch,
			Title:       item.Title,
			Tags:        models.DecodeTags(item.Tags),
			Timestamp:   ts,
			Confidence:  s.blendSemanticConfidence(semanticScore, ageScore),
			FileContent: strings.TrimSpace(item.Content),
		})
	}
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
	hitFetchLimit, err := s.semanticHitFetchLimitForQuery(store, source, projectName)
	if err != nil {
		return nil, err
	}
	best := make([]semanticCandidate, 0, hitFetchLimit)
	maxCandidateCount, err := s.semanticCandidateMaxCountForQuery(store, source, projectName)
	if err != nil {
		return nil, err
	}
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
			ageScore := s.confidenceByAgeForType(source, ts, now)
			candidate := semanticCandidate{
				MemoryID:    item.MemoryID,
				Timestamp:   ts,
				SemanticRaw: semanticScore,
				Confidence:  s.blendSemanticConfidence(semanticScore, ageScore),
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

// keywordMode 返回关键字检索模式，确保 LIKE 与 BM25 可以按配置切换。
func (s *Service) keywordMode() string {
	if s.config.SearchConfig == nil {
		return "like"
	}
	if strings.EqualFold(strings.TrimSpace(s.config.SearchConfig.KeywordMode), "bm25") {
		return "bm25"
	}
	return "like"
}

// keywordQueries 基于配置扩展同义词查询，避免调用方自己维护扩展规则。
func (s *Service) keywordQueries(queries []string) []string {
	cleaned := normalizePlainQueries(queries)
	if len(cleaned) == 0 {
		return cleaned
	}
	if s.config.SearchConfig == nil || !s.config.SearchConfig.KeywordSynonymsEnabled || len(s.config.SearchConfig.KeywordSynonymGroups) == 0 {
		return cleaned
	}
	out := make([]string, 0, len(cleaned))
	seen := map[string]struct{}{}
	for _, query := range cleaned {
		normalized := strings.ToLower(strings.TrimSpace(query))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; !ok {
			out = append(out, query)
			seen[normalized] = struct{}{}
		}
		for _, group := range s.config.SearchConfig.KeywordSynonymGroups {
			if !containsKeywordIgnoreCase(group, query) {
				continue
			}
			for _, item := range group {
				candidate := strings.TrimSpace(item)
				if candidate == "" {
					continue
				}
				candidateKey := strings.ToLower(candidate)
				if _, ok := seen[candidateKey]; ok {
					continue
				}
				out = append(out, candidate)
				seen[candidateKey] = struct{}{}
			}
		}
	}
	return out
}

// containsKeywordIgnoreCase 判断分组里是否包含查询词，避免同义词扩展误把无关分组加入召回条件。
func containsKeywordIgnoreCase(group []string, query string) bool {
	for _, item := range group {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(query)) {
			return true
		}
	}
	return false
}

// rankRowsByKeywordBM25 按配置字段和权重执行 BM25 重排，避免关键字结果长期仅靠时间排序。
func (s *Service) rankRowsByKeywordBM25(rows []Row, queries []string) ([]Row, map[int64]float64) {
	if len(rows) == 0 {
		return rows, map[int64]float64{}
	}
	terms := extractKeywordTerms(queries)
	if len(terms) == 0 {
		return rows, map[int64]float64{}
	}
	fields, fieldWeights := s.keywordBM25FieldsAndWeights()
	if len(fields) == 0 {
		return rows, map[int64]float64{}
	}
	n := float64(len(rows))
	avgLen := make(map[string]float64, len(fields))
	df := make(map[string]map[string]float64, len(fields))
	for _, field := range fields {
		df[field] = map[string]float64{}
		totalLen := 0.0
		for _, row := range rows {
			text := keywordFieldText(row, field)
			lowerText := strings.ToLower(text)
			totalLen += float64(len([]rune(text)))
			for _, term := range terms {
				if strings.Contains(lowerText, term) {
					df[field][term]++
				}
			}
		}
		avg := totalLen / n
		if avg <= 0 {
			avg = 1
		}
		avgLen[field] = avg
	}
	k1, b := s.keywordBM25Params()
	maxScore := 0.0
	scoreByID := map[int64]float64{}
	for _, row := range rows {
		score := 0.0
		for _, field := range fields {
			weight := fieldWeights[field]
			if weight <= 0 {
				continue
			}
			text := strings.ToLower(keywordFieldText(row, field))
			docLen := float64(len([]rune(text)))
			if docLen <= 0 {
				docLen = 1
			}
			for _, term := range terms {
				tf := float64(strings.Count(text, term))
				if tf <= 0 {
					continue
				}
				docFreq := df[field][term]
				idf := math.Log(1 + (n-docFreq+0.5)/(docFreq+0.5))
				denominator := tf + k1*(1-b+b*(docLen/avgLen[field]))
				if denominator <= 0 {
					continue
				}
				score += weight * idf * ((tf * (k1 + 1)) / denominator)
			}
		}
		scoreByID[row.ID] = score
		if score > maxScore {
			maxScore = score
		}
	}
	normalized := map[int64]float64{}
	if maxScore <= 0 {
		for _, row := range rows {
			normalized[row.ID] = 0
		}
	} else {
		for _, row := range rows {
			normalized[row.ID] = scoreByID[row.ID] / maxScore
		}
	}
	sortedRows := append([]Row(nil), rows...)
	sort.Slice(sortedRows, func(i, j int) bool {
		left := normalized[sortedRows[i].ID]
		right := normalized[sortedRows[j].ID]
		if left == right {
			return parseTimestamp(sortedRows[i].Timestamp).After(parseTimestamp(sortedRows[j].Timestamp))
		}
		return left > right
	})
	return sortedRows, normalized
}

// keywordBM25FieldsAndWeights 返回 BM25 参与字段和权重，避免关键字打分字段散落在多处。
func (s *Service) keywordBM25FieldsAndWeights() ([]string, map[string]float64) {
	allowed := map[string]struct{}{"title": {}, "summary": {}, "tags": {}, "content": {}, "project_name": {}}
	defaultFields := []string{"title", "summary", "tags", "content"}
	defaultWeights := map[string]float64{"title": 2.0, "summary": 1.5, "tags": 1.5, "content": 1.0}
	if s.config.SearchConfig == nil {
		return defaultFields, defaultWeights
	}
	fields := make([]string, 0, len(s.config.SearchConfig.KeywordFields))
	for _, field := range s.config.SearchConfig.KeywordFields {
		normalized := strings.ToLower(strings.TrimSpace(field))
		if _, ok := allowed[normalized]; ok {
			fields = append(fields, normalized)
		}
	}
	if len(fields) == 0 {
		fields = defaultFields
	}
	weights := map[string]float64{}
	for key, value := range defaultWeights {
		weights[key] = value
	}
	for key, value := range s.config.SearchConfig.KeywordFieldWeights {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if _, ok := allowed[normalized]; !ok || value <= 0 {
			continue
		}
		weights[normalized] = value
	}
	return fields, weights
}

// keywordBM25Params 返回 BM25 参数，确保公式调优可以通过配置控制。
func (s *Service) keywordBM25Params() (float64, float64) {
	if s.config.SearchConfig == nil {
		return 1.2, 0.75
	}
	k1 := s.config.SearchConfig.KeywordBM25K1
	if k1 <= 0 {
		k1 = 1.2
	}
	b := s.config.SearchConfig.KeywordBM25B
	if b < 0 {
		b = 0
	}
	if b > 1 {
		b = 1
	}
	return k1, b
}

// keywordFieldText 返回指定字段正文，避免 BM25 打分阶段重复拼接字段分支。
func keywordFieldText(row Row, field string) string {
	switch field {
	case "title":
		return row.Title
	case "summary":
		return row.Summary
	case "tags":
		return strings.Join(models.DecodeTags(row.Tags), " ")
	case "content":
		return row.Content
	case "project_name":
		return row.ProjectName
	default:
		return ""
	}
}

// extractKeywordTerms 提取关键字项用于 BM25 打分，避免整句输入导致统计粒度过粗。
func extractKeywordTerms(queries []string) []string {
	seen := map[string]struct{}{}
	terms := []string{}
	for _, query := range queries {
		for _, item := range keywordTermRE.FindAllString(strings.ToLower(strings.TrimSpace(query)), -1) {
			term := strings.TrimSpace(item)
			if term == "" {
				continue
			}
			if _, ok := seen[term]; ok {
				continue
			}
			terms = append(terms, term)
			seen[term] = struct{}{}
		}
	}
	return terms
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

// semanticCandidateMaxCountForQuery 根据配置策略动态计算候选窗口，避免固定窗口在大小库下表现失衡。
func (s *Service) semanticCandidateMaxCountForQuery(store *models.Store, source, projectName string) (int, error) {
	if s.config.EmbeddingConfig == nil {
		return s.semanticCandidateMaxCount(), nil
	}
	if !strings.EqualFold(strings.TrimSpace(s.config.EmbeddingConfig.SemanticWindowMode), "dynamic") {
		base := s.config.EmbeddingConfig.SemanticWindowBaseMaxCount
		if base < 1 {
			base = s.semanticCandidateMaxCount()
		}
		return maxInt(base, 1), nil
	}
	candidateCount, err := store.CountMemoryEmbeddingsByProjectAndType(projectName, source)
	if err != nil {
		return 0, err
	}
	minCount := maxInt(s.config.EmbeddingConfig.SemanticWindowDynamicMin, 1)
	maxCount := s.config.EmbeddingConfig.SemanticWindowDynamicMax
	if maxCount < minCount {
		maxCount = minCount
	}
	ratio := s.config.EmbeddingConfig.SemanticWindowDynamicRatio
	if ratio <= 0 {
		ratio = 0.2
	}
	computed := int(math.Ceil(float64(candidateCount) * ratio))
	if computed <= 0 {
		computed = s.config.EmbeddingConfig.SemanticWindowBaseMaxCount
	}
	if computed < minCount {
		computed = minCount
	}
	if computed > maxCount {
		computed = maxCount
	}
	return computed, nil
}

// semanticHitFetchLimit 返回最终允许回表的候选上限，避免高分候选过多时再次拉大正文开销。
func (s *Service) semanticHitFetchLimit() int {
	maxCount := s.semanticCandidateMaxCount()
	if s.config.EmbeddingConfig == nil || s.config.EmbeddingConfig.SemanticHitFetchLimit < 1 {
		return minInt(64, maxCount)
	}
	return minInt(s.config.EmbeddingConfig.SemanticHitFetchLimit, maxCount)
}

// semanticHitFetchLimitForQuery 按查询维度约束回表上限，确保动态窗口生效后回表规模同步受控。
func (s *Service) semanticHitFetchLimitForQuery(store *models.Store, source, projectName string) (int, error) {
	if s.config.EmbeddingConfig == nil {
		return s.semanticHitFetchLimit(), nil
	}
	maxCount, err := s.semanticCandidateMaxCountForQuery(store, source, projectName)
	if err != nil {
		return 0, err
	}
	fetchLimit := s.config.EmbeddingConfig.SemanticHitFetchLimit
	if fetchLimit < 1 {
		fetchLimit = s.semanticHitFetchLimit()
	}
	if fetchLimit > maxCount {
		fetchLimit = maxCount
	}
	if fetchLimit < 1 {
		fetchLimit = 1
	}
	return fetchLimit, nil
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

// mergeHits 融合关键字与语义命中，优先保留关键字片段并用配置权重计算统一置信度。
func (s *Service) mergeHits(source string, keywordHits, semanticHits []Hit) []Hit {
	merged := make([]Hit, 0, len(keywordHits)+len(semanticHits))
	keywordByID := make(map[int64]Hit, len(keywordHits))
	semanticByID := make(map[int64]Hit, len(semanticHits))
	orderedIDs := make([]int64, 0, len(keywordHits)+len(semanticHits))
	seen := map[int64]struct{}{}
	for _, hit := range keywordHits {
		keywordByID[hit.ID] = hit
		if _, ok := seen[hit.ID]; !ok {
			orderedIDs = append(orderedIDs, hit.ID)
			seen[hit.ID] = struct{}{}
		}
	}
	for _, hit := range semanticHits {
		semanticByID[hit.ID] = hit
		if _, ok := seen[hit.ID]; !ok {
			orderedIDs = append(orderedIDs, hit.ID)
			seen[hit.ID] = struct{}{}
		}
	}
	now := time.Now().UTC()
	for _, memoryID := range orderedIDs {
		keywordHit, hasKeyword := keywordByID[memoryID]
		semanticHit, hasSemantic := semanticByID[memoryID]
		if !hasKeyword && !hasSemantic {
			continue
		}
		result := semanticHit
		if hasKeyword {
			result = keywordHit
		}
		if hasKeyword && result.FileContent == "" && semanticHit.FileContent != "" {
			result.FileContent = semanticHit.FileContent
		}
		result.Confidence = s.fusedConfidence(source, hasKeyword, keywordHit, hasSemantic, semanticHit, now)
		merged = append(merged, result)
	}
	return merged
}

// fusedConfidence 使用配置权重融合关键字分、语义分与时效分，避免排序策略在代码里固化。
func (s *Service) fusedConfidence(source string, hasKeyword bool, keywordHit Hit, hasSemantic bool, semanticHit Hit, now time.Time) float64 {
	keywordScore := 0.0
	if hasKeyword {
		keywordScore = keywordHit.Confidence
	}
	semanticScore := 0.0
	if hasSemantic {
		semanticScore = semanticHit.Confidence
	}
	if !s.fusionEnabled() {
		if semanticScore > keywordScore {
			return semanticScore
		}
		return keywordScore
	}
	if hasSemantic && semanticScore < s.fusionMinSemanticScore() {
		semanticScore = 0
	}
	refTS := keywordHit.Timestamp
	if !hasKeyword {
		refTS = semanticHit.Timestamp
	}
	if hasSemantic && semanticHit.Timestamp.After(refTS) {
		refTS = semanticHit.Timestamp
	}
	recencyScore := s.confidenceByAgeForType(source, refTS, now)
	keywordWeight, semanticWeight, recencyWeight := s.fusionWeights()
	totalWeight := keywordWeight + semanticWeight + recencyWeight
	if totalWeight <= 0 {
		if semanticScore > keywordScore {
			return semanticScore
		}
		return keywordScore
	}
	return (keywordScore*keywordWeight + semanticScore*semanticWeight + recencyScore*recencyWeight) / totalWeight
}

// fusionEnabled 返回是否启用融合评分，便于渐进式灰度上线新排序策略。
func (s *Service) fusionEnabled() bool {
	if s.config.SearchConfig == nil {
		return true
	}
	return s.config.SearchConfig.FusionEnabled
}

// fusionWeights 返回融合权重，确保关键字、语义和时效占比可由配置统一控制。
func (s *Service) fusionWeights() (float64, float64, float64) {
	if s.config.SearchConfig == nil {
		return 0.55, 0.45, 0.1
	}
	return maxFloat(0, s.config.SearchConfig.FusionKeywordWeight), maxFloat(0, s.config.SearchConfig.FusionSemanticWeight), maxFloat(0, s.config.SearchConfig.FusionRecencyWeight)
}

// fusionMinSemanticScore 返回语义最低有效分，避免弱语义命中在融合时过度抬升。
func (s *Service) fusionMinSemanticScore() float64 {
	if s.config.SearchConfig == nil || s.config.SearchConfig.FusionMinSemanticScore < 0 {
		return 0
	}
	return s.config.SearchConfig.FusionMinSemanticScore
}

// mergeListHits 把不同类型命中合并为管理列表结果，并按相关度优先、时间次之稳定排序。
func mergeListHits(groups ...[]Hit) []Hit {
	merged := make([]Hit, 0)
	bestByID := make(map[int64]Hit)
	for _, group := range groups {
		for _, hit := range group {
			existing, ok := bestByID[hit.ID]
			if !ok || hit.Confidence > existing.Confidence || (hit.Confidence == existing.Confidence && hit.Timestamp.After(existing.Timestamp)) {
				bestByID[hit.ID] = hit
			}
		}
	}
	for _, hit := range bestByID {
		merged = append(merged, hit)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Confidence == merged[j].Confidence {
			return merged[i].Timestamp.After(merged[j].Timestamp)
		}
		return merged[i].Confidence > merged[j].Confidence
	})
	return merged
}

// paginateHits 统一分页切片，避免列表查询模式在服务层和 HTTP 层重复处理边界。
func paginateHits(hits []Hit, page, pageSize int) []Hit {
	if len(hits) == 0 {
		return []Hit{}
	}
	start := (page - 1) * pageSize
	if start >= len(hits) {
		return []Hit{}
	}
	end := minInt(start+pageSize, len(hits))
	return hits[start:end]
}

// buildListItems 按命中顺序回表组装管理端列表项，避免 SQL IN 查询打乱搜索排序。
func (s *Service) buildListItems(store *models.Store, hits []Hit) ([]MemoryListItem, error) {
	if len(hits) == 0 {
		return []MemoryListItem{}, nil
	}
	memoryIDs := make([]int64, 0, len(hits))
	confidenceByID := make(map[int64]float64, len(hits))
	for _, hit := range hits {
		memoryIDs = append(memoryIDs, hit.ID)
		confidenceByID[hit.ID] = hit.Confidence
	}
	items, err := store.ListMemoriesByIDs(memoryIDs)
	if err != nil {
		return nil, err
	}
	itemByID := make(map[int64]models.Memory, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
	}
	ordered := make([]MemoryListItem, 0, len(hits))
	for _, hit := range hits {
		item, ok := itemByID[hit.ID]
		if !ok {
			continue
		}
		confidence := confidenceByID[hit.ID]
		ordered = append(ordered, MemoryListItem{Memory: item, Confidence: &confidence})
	}
	return ordered, nil
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
func confidenceByAge(ts, now time.Time, halfLifeDays float64) float64 {
	ageDays := math.Max(0, now.Sub(ts).Hours()/24)
	effectiveHalfLifeDays := halfLifeDays
	if effectiveHalfLifeDays <= 0 {
		effectiveHalfLifeDays = 1
	}
	return math.Pow(0.5, ageDays/effectiveHalfLifeDays)
}

// confidenceByAgeForType 按记忆类型应用不同的时间衰减，避免错误记忆和总结记忆使用同一时效曲线。
func (s *Service) confidenceByAgeForType(source string, ts, now time.Time) float64 {
	return confidenceByAge(ts, now, s.decayHalfLifeDays(source))
}

// decayHalfLifeDays 返回指定类型的半衰期，确保召回时效策略可完全由配置控制。
func (s *Service) decayHalfLifeDays(source string) float64 {
	if s.config.EmbeddingConfig == nil {
		return 30
	}
	if strings.EqualFold(strings.TrimSpace(source), "error") {
		if s.config.EmbeddingConfig.DecayErrorHalfLifeDays > 0 {
			return s.config.EmbeddingConfig.DecayErrorHalfLifeDays
		}
		return 90
	}
	if s.config.EmbeddingConfig.DecaySummaryHalfLifeDays > 0 {
		return s.config.EmbeddingConfig.DecaySummaryHalfLifeDays
	}
	return 30
}

// blendSemanticConfidence 按配置融合语义分与时效分，避免语义召回权重在代码里写死。
func (s *Service) blendSemanticConfidence(semanticScore, ageScore float64) float64 {
	if s.config.EmbeddingConfig == nil || !s.config.EmbeddingConfig.DecayEnabled {
		return semanticScore
	}
	ageWeight := s.config.EmbeddingConfig.DecayAgeWeight
	semanticWeight := s.config.EmbeddingConfig.DecaySemanticWeight
	if ageWeight < 0 {
		ageWeight = 0
	}
	if semanticWeight < 0 {
		semanticWeight = 0
	}
	if ageWeight+semanticWeight <= 0 {
		return semanticScore
	}
	weighted := (ageScore*ageWeight + semanticScore*semanticWeight) / (ageWeight + semanticWeight)
	return math.Max(semanticScore, weighted)
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

// normalizePlainQueries 统一清洗查询词，避免列表与搜索入口在空白处理上出现行为分叉。
func normalizePlainQueries(queries []string) []string {
	out := make([]string, 0, len(queries))
	for _, query := range queries {
		if cleaned := strings.TrimSpace(query); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

// normalizePagination 统一分页边界，避免管理端不同查询模式下出现页码和页大小漂移。
func normalizePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	pageSize = minInt(pageSize, 100)
	return page, pageSize
}

// computeTotalPages 统一页数计算，避免管理列表在普通浏览和搜索模式下出现分页口径不一致。
func computeTotalPages(total int64, pageSize int) int {
	if pageSize <= 0 {
		pageSize = 10
	}
	if total == 0 {
		return 0
	}
	pages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		pages++
	}
	return pages
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

// maxFloat 返回较大浮点值，避免融合权重规范化时重复编写边界判断。
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
