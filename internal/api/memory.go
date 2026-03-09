package api

// SearchRequest 统一搜索接口入参，避免脚本和服务端各自维护字段协议。
type SearchRequest struct {
	ProjectName string   `json:"project_name"`
	Queries     []string `json:"queries"`
	Debug       bool     `json:"debug"`
}

// SearchSnippet 描述单条命中片段，方便脚本在本地重建筛选后的输出。
type SearchSnippet struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content string `json:"content"`
}

// SearchHit 描述单条搜索命中，供脚本按分支规则做二次筛选。
type SearchHit struct {
	Source      string          `json:"source"`
	Path        string          `json:"path"`
	ProjectName string          `json:"project_name"`
	GitBranch   string          `json:"git_branch"`
	Timestamp   string          `json:"timestamp"`
	Confidence  float64         `json:"confidence"`
	Snippets    []SearchSnippet `json:"snippets"`
	FileContent string          `json:"file_content"`
}

// SearchResponse 直接返回渲染后的 Markdown，减少脚本侧的结果拼装逻辑。
type SearchResponse struct {
	Query         string      `json:"query"`
	ProjectName   string      `json:"project_name"`
	DebugCommands []string    `json:"debug_commands,omitempty"`
	ErrorHits     []SearchHit `json:"error_hits"`
	SummaryHits   []SearchHit `json:"summary_hits"`
	Markdown      string      `json:"markdown"`
	Error         string      `json:"error,omitempty"`
}

// MemoryWriteItem 描述单条待写入记忆，便于批量写入沿用统一结构。
type MemoryWriteItem struct {
	Type    string   `json:"type"`
	Title   string   `json:"title"`
	Tags    []string `json:"tags"`
	Summary string   `json:"summary"`
	Context string   `json:"context"`
}

// WriteRequest 承接写入接口请求体，让脚本只传递必要业务参数。
type WriteRequest struct {
	ProjectName string            `json:"project_name"`
	GitBranch   string            `json:"git_branch"`
	Items       []MemoryWriteItem `json:"items"`
}

// WriteResponse 统一承接写入接口出参，成功时不再暴露数据库路径等内部细节。
type WriteResponse struct {
	Error string `json:"error,omitempty"`
}
