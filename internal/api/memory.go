package api

// SearchRequest 统一搜索接口入参，避免脚本和服务端各自维护字段协议。
type SearchRequest struct {
	ProjectRoot string   `json:"project_root"`
	Queries     []string `json:"queries"`
	Debug       bool     `json:"debug"`
}

// SearchResponse 直接返回渲染后的 Markdown，减少脚本侧的结果拼装逻辑。
type SearchResponse struct {
	Markdown string `json:"markdown"`
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
	ProjectRoot string            `json:"project_root"`
	Items       []MemoryWriteItem `json:"items"`
}

// WriteResponse 返回最终落盘数据库路径，方便脚本保持旧输出习惯。
type WriteResponse struct {
	DatabasePath string `json:"database_path"`
}

// RebuildRequest 描述向量重建请求，让脚本可以通过 HTTP 触发服务端维护动作。
type RebuildRequest struct {
	ProjectRoot string `json:"project_root"`
	Force       bool   `json:"force"`
}

// RebuildResponse 返回是否执行与说明信息，方便命令行直接展示结果。
type RebuildResponse struct {
	Changed bool   `json:"changed"`
	Message string `json:"message"`
}
