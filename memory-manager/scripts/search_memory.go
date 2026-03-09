package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	timestampRegexp = regexp.MustCompile(`^(\d{14})`)
	headingRegexp   = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
)

// MemoryHit 统一描述检索命中结构，便于关键字和语义召回复用同一渲染层。
type MemoryHit struct {
	ID          int64
	Source      string
	Path        string
	ProjectName string
	Timestamp   time.Time
	Confidence  float64
	Snippets    []Snippet
	FileContent string
	Header      map[string]any
}

// Snippet 统一描述命中片段与行号范围，方便 CLI 输出稳定可读。
type Snippet struct {
	Start   int
	End     int
	Content string
}

// runSearchCommand 按配置解析记忆目录后，从 SQLite 中执行检索并输出 Markdown。
func runSearchCommand(args []string) error {
	fs := flag.NewFlagSet("search-memory", flag.ContinueOnError)
	root := fs.String("root", ".", "Project root that contains .memory/")
	queriesArg := multiStringFlag{}
	fs.Var(&queriesArg, "query", "Search keyword/regex list")
	debug := fs.Bool("debug", false, "Print executed search commands")
	if err := fs.Parse(normalizeLegacySearchArgs(args)); err != nil {
		return err
	}
	queries, err := parseQueries(queriesArg)
	if err != nil {
		return err
	}
	if len(queries) == 0 {
		return fmt.Errorf("--query must contain at least one non-empty keyword")
	}

	location, err := resolveMemoryLocation(*root)
	if err != nil {
		return err
	}
	if _, err := rebuildEmbeddings(location.ProjectRoot, false); err != nil {
		return err
	}
	db, err := connectMemoryDB(location.MemoryRoot)
	if err != nil {
		return err
	}
	defer db.Close()

	allowLegacyBlankProject := !location.ExternalEnabled
	provider := createEmbeddingProvider(location.EmbeddingConfig)
	var debugCommands []string
	if *debug {
		debugCommands = []string{}
	}
	errorKeywordHits, err := collectHits(db, "error", location, allowLegacyBlankProject, queries, matcherForQueries(queries), &debugCommands)
	if err != nil {
		return err
	}
	summaryKeywordHits, err := collectHits(db, "summary", location, allowLegacyBlankProject, queries, matcherForQueries(queries), &debugCommands)
	if err != nil {
		return err
	}
	errorSemanticHits, err := collectSemanticHits(db, "error", location, allowLegacyBlankProject, queries, provider, &debugCommands)
	if err != nil {
		return err
	}
	summarySemanticHits, err := collectSemanticHits(db, "summary", location, allowLegacyBlankProject, queries, provider, &debugCommands)
	if err != nil {
		return err
	}
	fmt.Print(renderResultMarkdown(strings.Join(queries, ", "), location.SearchRoot, mergeHits(errorKeywordHits, errorSemanticHits), mergeHits(summaryKeywordHits, summarySemanticHits), debugCommands))
	return nil
}

// normalizeLegacySearchArgs 兼容旧入口里使用的 `-debug` 写法，减少迁移成本。
func normalizeLegacySearchArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-debug" {
			normalized = append(normalized, "--debug")
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

// multiStringFlag 兼容 flag 包的多次 --query 传参写法。
type multiStringFlag []string

// String 返回调试用字符串，满足 flag.Value 接口要求。
func (m *multiStringFlag) String() string { return strings.Join(*m, ",") }

// Set 允许重复传入相同参数，保留旧命令习惯。
func (m *multiStringFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

// parseQueries 兼容 JSON 数组和多参数写法，避免调用方因格式不同失败。
func parseQueries(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) == 1 {
		text := strings.TrimSpace(raw[0])
		if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			var payload []any
			if err := json.Unmarshal([]byte(text), &payload); err == nil {
				out := make([]string, 0, len(payload))
				for _, item := range payload {
					if query := strings.TrimSpace(asString(item)); query != "" {
						out = append(out, query)
					}
				}
				if len(out) > 0 {
					return out, nil
				}
			}
		}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if query := strings.TrimSpace(item); query != "" {
			out = append(out, query)
		}
	}
	return out, nil
}

// parseTimestamp 优先使用数据库内时间戳，保证排序与展示一致。
func parseTimestamp(timestamp string) time.Time {
	if match := timestampRegexp.FindStringSubmatch(strings.TrimSpace(timestamp)); len(match) == 2 {
		if parsed, err := time.ParseInLocation("20060102150405", match[1], time.UTC); err == nil {
			return parsed
		}
	}
	return time.Now().UTC()
}

// confidenceByAge 继续沿用时间衰减规则，让旧记忆自然降权而不是直接丢弃。
func confidenceByAge(ts time.Time, now time.Time) float64 {
	ageDays := math.Max(0, now.Sub(ts).Hours()/24)
	return math.Pow(0.5, ageDays/30)
}

// collectHits 在数据库记录中筛选关键字命中，并保留旧输出结构。
func collectHits(db *sql.DB, source string, location MemoryLocation, allowLegacyBlankProject bool, queries []string, matcher lineMatcher, debugCommands *[]string) ([]MemoryHit, error) {
	if debugCommands != nil {
		*debugCommands = append(*debugCommands, fmt.Sprintf("sqlite scan: %s [%s/%s]", getMemoryDBPath(location.MemoryRoot), location.ProjectName, source))
	}
	rows, err := fetchMemoryRowsByType(db, location.ProjectName, source, allowLegacyBlankProject)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hits := make([]MemoryHit, 0)
	for _, row := range rows {
		lines := splitLines(row.Content)
		header := readHeader(lines)
		bodyStart := bodyStartIndex(lines)
		lineNumbers := matchLineNumbers(lines, matcher)
		headerLineMatches := headerMatchLineNumbers(lineNumbers, bodyStart)
		headerTitleMatch := matchHeaderTitle(header, matcher)
		headerFieldMatch := matchHeaderFields(header, matcher)
		bodyMatches := bodyMatchLineNumbers(lineNumbers, bodyStart)
		if len(bodyMatches) == 0 && len(headerLineMatches) == 0 && !headerFieldMatch {
			continue
		}
		hit := MemoryHit{ID: row.ID, Source: source, Path: fmt.Sprintf("%s#project=%s#id=%d", getMemoryDBPath(location.MemoryRoot), row.ProjectName, row.ID), ProjectName: row.ProjectName, Timestamp: parseTimestamp(row.Timestamp), Confidence: confidenceByAge(parseTimestamp(row.Timestamp), now), Header: header}
		if headerTitleMatch {
			hit.FileContent = strings.TrimSpace(row.Content)
		} else if len(bodyMatches) > 0 {
			hit.Snippets = buildBodySectionSnippets(lines, bodyStart, bodyMatches, matcher)
		}
		if len(hit.Snippets) == 0 && hit.FileContent == "" {
			hit.Snippets = buildHeaderSnippets(lines, header, matcher, headerLineMatches)
		}
		if len(hit.Snippets) == 0 && hit.FileContent == "" {
			continue
		}
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Timestamp.After(hits[j].Timestamp) })
	return hits, nil
}

// collectSemanticHits 在关键字检索之外补充语义召回，减少措辞变化带来的漏检。
func collectSemanticHits(db *sql.DB, source string, location MemoryLocation, allowLegacyBlankProject bool, queries []string, provider EmbeddingProvider, debugCommands *[]string) ([]MemoryHit, error) {
	if !provider.Enabled() {
		return nil, nil
	}
	queryText := buildQueryEmbeddingText(source, queries)
	if strings.TrimSpace(queryText) == "" {
		return nil, nil
	}
	vectors, err := provider.EmbedTexts([]string{queryText})
	if err != nil || len(vectors) == 0 {
		return nil, err
	}
	rows, err := fetchMemoryRowsByType(db, location.ProjectName, source, allowLegacyBlankProject)
	if err != nil {
		return nil, err
	}
	memoryIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		memoryIDs = append(memoryIDs, row.ID)
	}
	embeddingMap, err := fetchMemoryEmbeddings(db, memoryIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hits := make([]MemoryHit, 0)
	for _, row := range rows {
		vector, ok := embeddingMap[row.ID]
		if !ok {
			continue
		}
		semanticScore := cosineSimilarity(vectors[0], vector)
		if semanticScore <= 0.15 {
			continue
		}
		ts := parseTimestamp(row.Timestamp)
		ageScore := confidenceByAge(ts, now)
		hits = append(hits, MemoryHit{ID: row.ID, Source: source, Path: fmt.Sprintf("%s#project=%s#id=%d", getMemoryDBPath(location.MemoryRoot), row.ProjectName, row.ID), ProjectName: row.ProjectName, Timestamp: ts, Confidence: math.Max(semanticScore, ageScore*0.5+semanticScore*0.5), FileContent: strings.TrimSpace(row.Content), Header: readHeader(splitLines(row.Content))})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Confidence == hits[j].Confidence {
			return hits[i].Timestamp.After(hits[j].Timestamp)
		}
		return hits[i].Confidence > hits[j].Confidence
	})
	return hits, nil
}

// mergeHits 关键字命中优先保留原片段展示，再补上语义召回缺失结果。
func mergeHits(primaryHits, semanticHits []MemoryHit) []MemoryHit {
	merged := make([]MemoryHit, 0, len(primaryHits)+len(semanticHits))
	seen := map[int64]struct{}{}
	semanticByID := map[int64]MemoryHit{}
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

// matcherForQueries 同时支持正则与大小写不敏感子串匹配。
func matcherForQueries(queries []string) lineMatcher {
	patterns := make([]*regexp.Regexp, 0, len(queries))
	fallback := make([]string, 0, len(queries))
	for _, query := range queries {
		if pattern, err := regexp.Compile("(?i)" + query); err == nil {
			patterns = append(patterns, pattern)
		} else {
			fallback = append(fallback, strings.ToLower(query))
		}
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

// matchLineNumbers 按行匹配全文，继续复用原有片段截取逻辑。
func matchLineNumbers(lines []string, matcher lineMatcher) []int {
	out := make([]int, 0)
	for idx, line := range lines {
		if matcher(line) {
			out = append(out, idx+1)
		}
	}
	return out
}

// bodyStartIndex 定位 YAML 头部结束位置，便于区分元数据与正文匹配。
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
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if tag := strings.TrimSpace(asString(parseScalar(part))); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// readHeader 从持久化的 Markdown 内容中恢复头部字段。
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
		} else {
			header[key] = parseScalar(raw)
		}
	}
	return header
}

// matchHeaderFields 头部字段单独参与匹配，避免正文为空时漏掉标题型记忆。
func matchHeaderFields(header map[string]any, matcher lineMatcher) bool {
	for _, key := range []string{"project", "title", "summary"} {
		if value, ok := header[key].(string); ok && value != "" && matcher(value) {
			return true
		}
	}
	if tags, ok := header["tags"].([]string); ok {
		for _, tag := range tags {
			if matcher(tag) {
				return true
			}
		}
	}
	return false
}

// matchHeaderTitle 标题命中时返回全文，方便快速回看完整结论。
func matchHeaderTitle(header map[string]any, matcher lineMatcher) bool {
	value, ok := header["title"].(string)
	return ok && value != "" && matcher(value)
}

func bodyMatchLineNumbers(lineNumbers []int, bodyStart int) []int {
	out := []int{}
	for _, lineNo := range lineNumbers {
		if lineNo-1 >= bodyStart {
			out = append(out, lineNo)
		}
	}
	return out
}
func headerMatchLineNumbers(lineNumbers []int, bodyStart int) []int {
	out := []int{}
	for _, lineNo := range lineNumbers {
		if lineNo-1 < bodyStart {
			out = append(out, lineNo)
		}
	}
	return out
}
func isHeadingLine(line string) bool { return headingRegexp.MatchString(strings.TrimSpace(line)) }
func headingLevel(line string) int {
	match := headingRegexp.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) < 2 {
		return 0
	}
	return len(match[1])
}
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
	seen := map[string]struct{}{}
	snippets := make([]Snippet, 0)
	for _, lineNo := range matchLines {
		idx := lineNo - 1 - bodyStart
		if idx < 0 || idx >= len(bodyLines) {
			continue
		}
		content, startIdx, endIdx := "", 0, 0
		if isHeadingLine(bodyLines[idx]) && matcher(headingText(bodyLines[idx])) {
			content, startIdx, endIdx = extractSection(bodyLines, idx)
		} else if parent := findParentHeadingIndex(bodyLines, idx); parent >= 0 {
			content, startIdx, endIdx = extractSection(bodyLines, parent)
		} else {
			content, startIdx, endIdx = buildBodySnippet(lines, bodyStart, lineNo)
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

func sectionEndIndex(bodyLines []string, headingIdx int) int {
	startLevel := headingLevel(bodyLines[headingIdx])
	for idx := headingIdx + 1; idx < len(bodyLines); idx++ {
		if isHeadingLine(bodyLines[idx]) && headingLevel(bodyLines[idx]) <= startLevel {
			return idx
		}
	}
	return len(bodyLines)
}
func extractSection(bodyLines []string, headingIdx int) (string, int, int) {
	endIdx := sectionEndIndex(bodyLines, headingIdx)
	return strings.TrimSpace(strings.Join(bodyLines[headingIdx:endIdx], "\n")), headingIdx, endIdx
}
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
	snippets := make([]Snippet, 0)
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
	return snippets
}

// renderResultMarkdown 输出最终 Markdown，兼容现有 skill 的结果消费方式。
func renderResultMarkdown(query, searchRoot string, errorHits, summaryHits []MemoryHit, debugCommands []string) string {
	lines := []string{"# Memory Search Result", "- query: " + query, "- search_root: " + searchRoot}
	if len(debugCommands) > 0 {
		lines = append(lines, "- debug: true", "", "## Debug Commands")
		for _, cmd := range debugCommands {
			lines = append(lines, "- `"+cmd+"`")
		}
	}
	lines = append(lines, "", renderHitsSection("Error Hits", errorHits), "", renderHitsSection("Summary Hits", summaryHits))
	return strings.Join(lines, "\n") + "\n"
}

func renderHitsSection(title string, hits []MemoryHit) string {
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

// renderHitMarkdown 渲染单条命中结果，保持 CLI 输出稳定且可扫描。
func renderHitMarkdown(hit MemoryHit, index int) string {
	lines := []string{fmt.Sprintf("### Record %d", index), "- source: " + hit.Source, "- path: " + hit.Path}
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

func markdownFenceFor(text string) string {
	if strings.Contains(text, "```") {
		return "````"
	}
	return "```"
}
func splitLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
