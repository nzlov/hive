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
	GitBranch   string          `json:"git_branch"`
	Title       string          `json:"title"`
	Tags        []string        `json:"tags"`
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

// UserSummary 统一描述当前登录用户，避免前后端各自维护字段名。
type UserSummary struct {
	ID       int64  `json:"id"`
	UserID   string `json:"userid"`
	Username string `json:"username"`
	RealName string `json:"real_name"`
	APIToken string `json:"apitoken"`
	IsAdmin  bool   `json:"is_admin"`
}

// LoginRequest 描述管理界面登录入参，保持鉴权入口协议稳定。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse 统一返回 JWT 和当前用户信息，减少前端额外探测请求。
type LoginResponse struct {
	Token string      `json:"token,omitempty"`
	User  UserSummary `json:"user,omitempty"`
	Error string      `json:"error,omitempty"`
}

// UserListResponse 为用户管理页提供稳定列表结构，避免直接暴露数据库字段。
type UserListResponse struct {
	Items []UserSummary `json:"items"`
	Error string        `json:"error,omitempty"`
}

// CreateUserRequest 描述新增用户时可编辑字段，让服务端统一生成 userid 与 apitoken。
type CreateUserRequest struct {
	Username string `json:"username"`
	RealName string `json:"real_name"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

// UpdateUserRequest 描述用户更新入参，允许后台按需重置密码或 token。
type UpdateUserRequest struct {
	Username        string `json:"username"`
	RealName        string `json:"real_name"`
	Password        string `json:"password"`
	IsAdmin         bool   `json:"is_admin"`
	RegenerateToken bool   `json:"regenerate_token"`
}

// UserMutationResponse 统一承接用户写操作结果，方便前端直接刷新最新对象。
type UserMutationResponse struct {
	Item  UserSummary `json:"item,omitempty"`
	Error string      `json:"error,omitempty"`
}

// MemoryListRequest 描述后台记忆列表查询参数，确保分页和关键字筛选协议稳定。
type MemoryListRequest struct {
	Page     int      `form:"page"`
	PageSize int      `form:"page_size"`
	Queries  []string `form:"queries"`
}

// MemoryItem 描述记忆列表展示项，避免列表接口返回过大的正文内容。
type MemoryItem struct {
	ID          int64    `json:"id"`
	ProjectName string   `json:"project_name"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Summary     string   `json:"summary"`
	Confidence  *float64 `json:"confidence,omitempty"`
	UserID      string   `json:"userid"`
	CreatedAt   string   `json:"created_at"`
}

// MemoryDetail 描述单条记忆详情，供管理端查看完整上下文而不额外拼装字段。
type MemoryDetail struct {
	ID          int64    `json:"id"`
	ProjectName string   `json:"project_name"`
	GitBranch   string   `json:"git_branch"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Summary     string   `json:"summary"`
	Content     string   `json:"content"`
	UserID      string   `json:"userid"`
	Timestamp   string   `json:"timestamp"`
	CreatedAt   string   `json:"created_at"`
}

// MemoryListResponse 统一返回分页结果，避免前端自行推断总页数。
type MemoryListResponse struct {
	Items     []MemoryItem `json:"items"`
	Total     int64        `json:"total"`
	Page      int          `json:"page"`
	PageSize  int          `json:"page_size"`
	TotalPage int          `json:"total_page"`
	Error     string       `json:"error,omitempty"`
}

// MemoryDetailResponse 承接单条记忆详情响应，便于接口错误时保持统一结构。
type MemoryDetailResponse struct {
	Item  MemoryDetail `json:"item,omitempty"`
	Error string       `json:"error,omitempty"`
}
