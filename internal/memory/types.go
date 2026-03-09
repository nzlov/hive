package memory

import "time"

// Location 描述当前项目使用的记忆目录和检索维度，避免路径决策散落在多个层次。
type Location struct {
	ProjectRoot     string
	ProjectName     string
	SearchRoot      string
	MemoryRoot      string
	ExternalEnabled bool
}

// Row 统一描述数据库里的记忆记录，避免搜索和写入层重复维护字段。
type Row struct {
	ID          int64
	ProjectName string
	Type        string
	Title       string
	Tags        string
	Summary     string
	Content     string
	Timestamp   string
	CreatedAt   string
}

// Hit 描述单条命中结果，让关键字检索和语义召回共享同一渲染模型。
type Hit struct {
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

// Snippet 描述命中的片段及其行号范围，便于输出稳定的 Markdown 结构。
type Snippet struct {
	Start   int
	End     int
	Content string
}

// RebuildResult 统一表达向量重建结果，方便 HTTP 接口和内部逻辑复用。
type RebuildResult struct {
	Changed bool
	Message string
}
