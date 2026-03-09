package memory

import (
	"database/sql"
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
func (s *Service) Search(projectName string, queries []string, debug bool) (SearchResult, error) {
	location, db, err := s.openProjectDB()
	if err != nil {
		return SearchResult{}, err
	}
	defer db.Close()
	effectiveProjectName := normalizeProjectName(projectName)
	var debugCommands []string
	var debugCommandsRef *[]string
	if debug {
		debugCommands = []string{}
		debugCommandsRef = &debugCommands
	}
	matcher := buildQueryMatcher(queries)
	errorKeywordHits, err := s.collectHits(db, "error", location, effectiveProjectName, matcher, debugCommandsRef)
	if err != nil {
		return SearchResult{}, err
	}
	summaryKeywordHits, err := s.collectHits(db, "summary", location, effectiveProjectName, matcher, debugCommandsRef)
	if err != nil {
		return SearchResult{}, err
	}
	errorSemanticHits, err := s.collectSemanticHits(db, "error", location, effectiveProjectName, queries)
	if err != nil {
		return SearchResult{}, err
	}
	summarySemanticHits, err := s.collectSemanticHits(db, "summary", location, effectiveProjectName, queries)
	if err != nil {
		return SearchResult{}, err
	}
	result := SearchResult{
		Query:         strings.Join(queries, ", "),
		ProjectName:   effectiveProjectName,
		DebugCommands: debugCommands,
		ErrorHits:     mergeHits(errorKeywordHits, errorSemanticHits),
		SummaryHits:   mergeHits(summaryKeywordHits, summarySemanticHits),
	}
	return result, nil
}

// Write 写入总结或错误记忆，并把写入人 userid 一并落库以便后续追溯来源。
func (s *Service) Write(projectName, gitBranch, userID string, items []api.MemoryWriteItem) (string, error) {
	location, db, err := s.openProjectDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	timestampSeed := now.Unix()
	effectiveProjectName := normalizeProjectName(projectName)
	normalizedGitBranch := normalizeGitBranch(gitBranch)
	rows := make([]Row, 0, len(items))
	ids := make([]int64, 0, len(items))
	for idx, item := range items {
		itemTime := time.Unix(timestampSeed+int64(idx), 0).UTC()
		row := Row{
			UserID:      strings.TrimSpace(userID),
			ProjectName: effectiveProjectName,
			Type:        strings.TrimSpace(item.Type),
			Title:       sanitizeTitle(item.Title),
			Tags:        EncodeTags(item.Tags),
			Summary:     strings.TrimSpace(item.Summary),
			Content:     buildMemoryContent(effectiveProjectName, normalizedGitBranch, strings.TrimSpace(userID), item),
			Timestamp:   itemTime.Format("20060102150405"),
			CreatedAt:   itemTime.Format(time.RFC3339Nano),
		}
		memoryID, err := InsertMemory(tx, row)
		if err != nil {
			return "", err
		}
		row.ID = memoryID
		rows = append(rows, row)
		ids = append(ids, memoryID)
	}
	if s.provider.Enabled() && len(rows) > 0 {
		texts := make([]string, 0, len(rows))
		for _, row := range rows {
			texts = append(texts, BuildMemoryEmbeddingText(row))
		}
		log.Printf("开始为 %d 条记忆生成向量...", len(texts))
		vectors, err := s.provider.EmbedTexts(texts)
		if err != nil {
			return "", err
		}
		updatedAt := now.Format(time.RFC3339Nano)
		for idx, memoryID := range ids {
			if idx >= len(vectors) {
				break
			}
			if err := UpsertMemoryEmbedding(tx, effectiveProjectName, memoryID, vectors[idx], updatedAt); err != nil {
				return "", err
			}
		}
		if err := SetMemoryMetadata(tx, embeddingModelMetaKey, s.provider.ModelName(), updatedAt); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return DBPath(location.MemoryRoot), nil
}

// EnsureEmbeddingsReady 在服务启动阶段校验模型一致性，避免请求到来后才暴露旧向量问题。
func (s *Service) EnsureEmbeddingsReady() (RebuildResult, error) {
	location, db, err := s.openProjectDB()
	if err != nil {
		return RebuildResult{}, err
	}
	defer db.Close()
	return s.rebuildEmbeddingsWithDB(location, db, false)
}

// openProjectDB 统一完成数据库连接，避免重复打开逻辑散落在各能力中。
func (s *Service) openProjectDB() (Location, *sql.DB, error) {
	location := ResolveLocation(s.config)
	db, err := ConnectDB(location.MemoryRoot)
	if err != nil {
		return Location{}, nil, err
	}
	return location, db, nil
}

// rebuildEmbeddingsWithDB 在模型变化时全量重建向量，避免新旧维度混用。
func (s *Service) rebuildEmbeddingsWithDB(location Location, db *sql.DB, force bool) (RebuildResult, error) {
	if !s.provider.Enabled() {
		return RebuildResult{Changed: false, Message: "未配置嵌入模型，跳过重建。"}, nil
	}
	currentModel, err := GetMemoryMetadata(db, embeddingModelMetaKey)
	if err != nil {
		return RebuildResult{}, err
	}
	rows, err := FetchAllMemories(db)
	if err != nil {
		return RebuildResult{}, err
	}
	var embeddingCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_embeddings`).Scan(&embeddingCount); err != nil {
		return RebuildResult{}, err
	}
	if currentModel == s.provider.ModelName() && embeddingCount == len(rows) && !force {
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
	tx, err := db.Begin()
	if err != nil {
		return RebuildResult{}, err
	}
	defer tx.Rollback()
	if err := DeleteAllMemoryEmbeddings(tx); err != nil {
		return RebuildResult{}, err
	}
	rebuiltAt := time.Now().UTC().Format(time.RFC3339Nano)
	total := len(rows)
	for idx, row := range rows {
		if idx >= len(vectors) {
			break
		}
		if err := UpsertMemoryEmbedding(tx, row.ProjectName, row.ID, vectors[idx], rebuiltAt); err != nil {
			return RebuildResult{}, err
		}
		if (idx+1)%100 == 0 || idx+1 == total {
			log.Printf("索引重建进度: %d/%d (%.1f%%)", idx+1, total, float64(idx+1)*100/float64(total))
		}
	}
	if err := SetMemoryMetadata(tx, embeddingModelMetaKey, s.provider.ModelName(), rebuiltAt); err != nil {
		return RebuildResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RebuildResult{}, err
	}
	_ = location
	return RebuildResult{Changed: true, Message: fmt.Sprintf("已使用模型 %s 重建 %d 条向量。", s.provider.ModelName(), len(vectors))}, nil
}

// collectHits 在数据库记录中筛选关键字命中，并按项目名隔离单库里的不同项目数据。
func (s *Service) collectHits(db *sql.DB, source string, location Location, projectName string, matcher lineMatcher, debugCommands *[]string) ([]Hit, error) {
	if debugCommands != nil {
		*debugCommands = append(*debugCommands, fmt.Sprintf("sqlite scan: %s [%s/%s]", DBPath(location.MemoryRoot), projectName, source))
	}
	rows, err := FetchMemoryRows(db, projectName, source)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hits := make([]Hit, 0)
	for _, row := range rows {
		lines := splitLines(row.Content)
		header := readHeader(lines)
		rowGitBranch := readGitBranch(header)
		bodyStart := bodyStartIndex(lines)
		lineNumbers := matchLineNumbers(lines, matcher)
		headerLineMatches := headerMatchLineNumbers(lineNumbers, bodyStart)
		headerTitleMatch := matchHeaderTitle(header, matcher)
		headerFieldMatch := matchHeaderFields(header, matcher)
		bodyMatches := bodyMatchLineNumbers(lineNumbers, bodyStart)
		if len(bodyMatches) == 0 && len(headerLineMatches) == 0 && !headerFieldMatch {
			continue
		}
		ts := parseTimestamp(row.Timestamp)
		hit := Hit{ID: row.ID, Source: source, Path: fmt.Sprintf("%s#project=%s#id=%d", DBPath(location.MemoryRoot), row.ProjectName, row.ID), ProjectName: row.ProjectName, GitBranch: rowGitBranch, Timestamp: ts, Confidence: confidenceByAge(ts, now), Header: header}
		if headerTitleMatch {
			hit.FileContent = strings.TrimSpace(row.Content)
		} else if len(bodyMatches) > 0 {
			hit.Snippets = enrichSnippetsWithHeader(buildBodySectionSnippets(lines, bodyStart, bodyMatches, matcher), header)
		}
		if len(hit.Snippets) == 0 && hit.FileContent == "" {
			hit.Snippets = enrichSnippetsWithHeader(buildHeaderSnippets(lines, header, matcher, headerLineMatches), header)
		}
		if len(hit.Snippets) == 0 && hit.FileContent == "" {
			continue
		}
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Timestamp.After(hits[j].Timestamp) })
	return hits, nil
}

// collectSemanticHits 在关键字检索之外补充语义召回，并继续按项目名隔离结果。
func (s *Service) collectSemanticHits(db *sql.DB, source string, location Location, projectName string, queries []string) ([]Hit, error) {
	if !s.provider.Enabled() {
		return nil, nil
	}
	queryText := BuildQueryEmbeddingText(source, queries)
	if strings.TrimSpace(queryText) == "" {
		return nil, nil
	}
	vectors, err := s.provider.EmbedTexts([]string{queryText})
	if err != nil || len(vectors) == 0 {
		return nil, err
	}
	rows, err := FetchMemoryRows(db, projectName, source)
	if err != nil {
		return nil, err
	}
	filteredRows := make([]Row, 0, len(rows))
	memoryIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		filteredRows = append(filteredRows, row)
		memoryIDs = append(memoryIDs, row.ID)
	}
	embeddings, err := FetchMemoryEmbeddings(db, projectName, memoryIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hits := make([]Hit, 0)
	for _, row := range filteredRows {
		vector, ok := embeddings[row.ID]
		if !ok {
			continue
		}
		semanticScore := CosineSimilarity(vectors[0], vector)
		if semanticScore <= 0.15 {
			continue
		}
		ts := parseTimestamp(row.Timestamp)
		ageScore := confidenceByAge(ts, now)
		header := readHeader(splitLines(row.Content))
		hits = append(hits, Hit{ID: row.ID, Source: source, Path: fmt.Sprintf("%s#project=%s#id=%d", DBPath(location.MemoryRoot), row.ProjectName, row.ID), ProjectName: row.ProjectName, GitBranch: readGitBranch(header), Timestamp: ts, Confidence: math.Max(semanticScore, ageScore*0.5+semanticScore*0.5), FileContent: strings.TrimSpace(row.Content), Header: header})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Confidence == hits[j].Confidence {
			return hits[i].Timestamp.After(hits[j].Timestamp)
		}
		return hits[i].Confidence > hits[j].Confidence
	})
	return hits, nil
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

// buildMemoryContent 统一生成持久化 Markdown 内容，并把写入人标识写进头部便于审计定位。
func buildMemoryContent(projectName, gitBranch, userID string, item api.MemoryWriteItem) string {
	summary := strings.TrimSpace(item.Summary)
	if summary == "" {
		summary = "自动生成记忆"
	}
	headLines := []string{
		"---",
		"type: " + strings.TrimSpace(item.Type),
		"project: " + projectName,
		"title: " + sanitizeTitle(item.Title),
		"tags: " + strings.Join(item.Tags, ", "),
		"summary: " + summary,
	}
	if gitBranch != "" {
		headLines = append(headLines, "git_branch: "+gitBranch)
	}
	if userID != "" {
		headLines = append(headLines, "user_id: "+userID)
	}
	headLines = append(headLines, "---", "", markdownBody(item.Context), "")
	return strings.Join(headLines, "\n")
}

// markdownBody 确保持久化内容始终有正文，避免空记录影响后续检索体验。
func markdownBody(context string) string {
	body := strings.TrimSpace(context)
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
	if hit.ProjectName != "" {
		lines = append(lines, "- project: "+hit.ProjectName)
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

// bodyStartIndex 定位 YAML 头部结束位置，便于区分元数据和正文匹配。
func bodyStartIndex(lines []string) int {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0
	}
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			return idx + 1
		}
	}
	return 0
}

// parseScalar 按轻量规则解析头部值，避免强依赖完整 YAML 解析器。
func parseScalar(value string) any {
	text := strings.TrimSpace(value)
	if len(text) >= 2 && ((text[0] == '"' && text[len(text)-1] == '"') || (text[0] == '\'' && text[len(text)-1] == '\'')) {
		return text[1 : len(text)-1]
	}
	return text
}

// parseTags 兼容逗号分隔和列表字符串，降低旧内容兼容成本。
func parseTags(value string) []string {
	text := strings.TrimSpace(value)
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := []string{}
	for _, part := range parts {
		tag := strings.TrimSpace(fmt.Sprint(parseScalar(part)))
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// readHeader 从持久化的 Markdown 内容中恢复头部字段，方便后续做标题和标签匹配。
func readHeader(lines []string) map[string]any {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]any{}
	}
	end := -1
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			end = idx
			break
		}
	}
	if end == -1 {
		return map[string]any{}
	}
	header := map[string]any{}
	for _, line := range lines[1:end] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		raw := strings.TrimSpace(parts[1])
		if key == "tags" {
			header[key] = parseTags(raw)
			continue
		}
		header[key] = parseScalar(raw)
	}
	return header
}

// matchHeaderFields 让头部字段单独参与匹配，避免正文为空时漏掉标题型记忆。
func matchHeaderFields(header map[string]any, matcher lineMatcher) bool {
	for _, key := range []string{"project", "title", "summary", "git_branch"} {
		value, ok := header[key].(string)
		if ok && value != "" && matcher(value) {
			return true
		}
	}
	tags, ok := header["tags"].([]string)
	if !ok {
		return false
	}
	for _, tag := range tags {
		if matcher(tag) {
			return true
		}
	}
	return false
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

// readGitBranch 从头部读取分支信息，让服务端和脚本共享同一字段语义。
func readGitBranch(header map[string]any) string {
	value, _ := header["git_branch"].(string)
	return normalizeGitBranch(value)
}

// matchHeaderTitle 标题命中时返回全文，方便快速回看完整结论。
func matchHeaderTitle(header map[string]any, matcher lineMatcher) bool {
	value, ok := header["title"].(string)
	return ok && value != "" && matcher(value)
}

// bodyMatchLineNumbers 过滤出正文命中行，避免头部匹配误入正文片段流程。
func bodyMatchLineNumbers(lineNumbers []int, bodyStart int) []int {
	out := []int{}
	for _, lineNo := range lineNumbers {
		if lineNo-1 >= bodyStart {
			out = append(out, lineNo)
		}
	}
	return out
}

// headerMatchLineNumbers 过滤出头部命中行，供元数据片段兜底展示。
func headerMatchLineNumbers(lineNumbers []int, bodyStart int) []int {
	out := []int{}
	for _, lineNo := range lineNumbers {
		if lineNo-1 < bodyStart {
			out = append(out, lineNo)
		}
	}
	return out
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

// buildBodySectionSnippets 正文优先按章节返回片段，让结论和约束一起出现。
func buildBodySectionSnippets(lines []string, bodyStart int, matchLines []int, matcher lineMatcher) []Snippet {
	bodyLines := lines[bodyStart:]
	if len(bodyLines) == 0 {
		return nil
	}
	snippets := []Snippet{}
	seen := map[string]struct{}{}
	for _, lineNo := range matchLines {
		idx := lineNo - 1 - bodyStart
		if idx < 0 || idx >= len(bodyLines) {
			continue
		}
		content := ""
		startIdx := 0
		endIdx := 0
		if isHeadingLine(bodyLines[idx]) && matcher(headingText(bodyLines[idx])) {
			content, startIdx, endIdx = extractSection(bodyLines, idx)
		} else {
			parent := findParentHeadingIndex(bodyLines, idx)
			if parent >= 0 {
				content, startIdx, endIdx = extractSection(bodyLines, parent)
			} else {
				content, startIdx, endIdx = buildBodySnippet(lines, bodyStart, lineNo)
			}
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		absStart := bodyStart + startIdx + 1
		absEnd := bodyStart + endIdx
		key := fmt.Sprintf("%d-%d", absStart, absEnd)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		snippets = append(snippets, Snippet{Start: absStart, End: absEnd, Content: content})
	}
	return snippets
}

// enrichSnippetsWithHeader 为片段补充标题与标签，避免脱离原记忆时难以理解命中上下文。
func enrichSnippetsWithHeader(snippets []Snippet, header map[string]any) []Snippet {
	if len(snippets) == 0 {
		return nil
	}
	prefixLines := []string{}
	if title, ok := header["title"].(string); ok {
		title = strings.TrimSpace(title)
		if title != "" {
			prefixLines = append(prefixLines, "title: "+title)
		}
	}
	if tags, ok := header["tags"].([]string); ok && len(tags) > 0 {
		cleanedTags := make([]string, 0, len(tags))
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				cleanedTags = append(cleanedTags, tag)
			}
		}
		if len(cleanedTags) > 0 {
			prefixLines = append(prefixLines, "tags: "+strings.Join(cleanedTags, ", "))
		}
	}
	if len(prefixLines) == 0 {
		return snippets
	}
	prefix := strings.Join(prefixLines, "\n") + "\n\n"
	decorated := make([]Snippet, 0, len(snippets))
	for _, snippet := range snippets {
		decorated = append(decorated, Snippet{
			Start:   snippet.Start,
			End:     snippet.End,
			Content: prefix + snippet.Content,
		})
	}
	return decorated
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
func buildBodySnippet(lines []string, bodyStart int, matchLine int) (string, int, int) {
	bodyLines := lines[bodyStart:]
	matchIdx := matchLine - 1 - bodyStart
	if matchIdx < 0 || matchIdx >= len(bodyLines) {
		return "", 0, 0
	}
	before := minInt(10, matchIdx)
	after := minInt(9, len(bodyLines)-matchIdx-1)
	total := before + 1 + after
	missing := 20 - total
	if missing > 0 {
		extraAfter := minInt(missing, len(bodyLines)-matchIdx-1-after)
		after += extraAfter
		missing -= extraAfter
	}
	if missing > 0 {
		before += minInt(missing, matchIdx-before)
	}
	start := matchIdx - before
	end := matchIdx + after + 1
	return strings.TrimSpace(strings.Join(bodyLines[start:end], "\n")), start, end
}

// buildHeaderSnippets 为仅命中头部字段的记录构建最小可读片段。
func buildHeaderSnippets(lines []string, header map[string]any, matcher lineMatcher, headerMatchLines []int) []Snippet {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	end := -1
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			end = idx
			break
		}
	}
	if end == -1 {
		return nil
	}
	snippets := []Snippet{}
	for lineNo := 2; lineNo <= end+1; lineNo++ {
		raw := lines[lineNo-1]
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if value == "" {
			continue
		}
		if (key == "title" || key == "summary") && matcher(value) {
			snippets = append(snippets, Snippet{Start: lineNo, End: lineNo, Content: key + ": " + value})
		}
		if key == "tags" {
			matched := []string{}
			for _, tag := range parseTags(value) {
				if matcher(tag) {
					matched = append(matched, tag)
				}
			}
			if len(matched) > 0 {
				snippets = append(snippets, Snippet{Start: lineNo, End: lineNo, Content: "tags: " + strings.Join(matched, ", ")})
			}
		}
	}
	if len(snippets) > 0 {
		return snippets
	}
	for _, lineNo := range headerMatchLines {
		if lineNo >= 1 && lineNo <= len(lines) {
			if content := strings.TrimSpace(lines[lineNo-1]); content != "" {
				snippets = append(snippets, Snippet{Start: lineNo, End: lineNo, Content: content})
			}
		}
	}
	if len(snippets) > 0 {
		return snippets
	}
	fallback := []string{}
	if title, ok := header["title"].(string); ok && title != "" {
		fallback = append(fallback, "title: "+title)
	}
	if tags, ok := header["tags"].([]string); ok && len(tags) > 0 {
		fallback = append(fallback, "tags: "+strings.Join(tags, ", "))
	}
	if summary, ok := header["summary"].(string); ok && summary != "" {
		fallback = append(fallback, "summary: "+summary)
	}
	if len(fallback) == 0 {
		return nil
	}
	return []Snippet{{Start: 1, End: end + 1, Content: strings.Join(fallback, "\n")}}
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
