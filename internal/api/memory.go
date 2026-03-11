package api

// SearchRequest 统一搜索接口入参，避免脚本和服务端各自维护字段协议。
type SearchRequest struct {
	ProjectName string   `json:"project_name"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
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

// UserListRequest 描述用户列表查询参数，确保分页与关键字搜索协议稳定。
type UserListRequest struct {
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	Keyword  string `form:"keyword"`
}

// UserListResponse 为用户管理页提供稳定分页结构，避免前端自行推断总页数。
type UserListResponse struct {
	Items     []UserSummary `json:"items"`
	Total     int64         `json:"total"`
	Page      int           `json:"page"`
	PageSize  int           `json:"page_size"`
	TotalPage int           `json:"total_page"`
	Error     string        `json:"error,omitempty"`
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
	Page        int      `form:"page"`
	PageSize    int      `form:"page_size"`
	Tags        []string `form:"tags"`
	Description string   `form:"description"`
}

// MemoryItem 描述记忆列表展示项，避免列表接口返回过大的正文内容。
type MemoryItem struct {
	ID          int64    `json:"id"`
	ProjectName string   `json:"project_name"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Summary     string   `json:"summary"`
	Confidence  *float64 `json:"confidence,omitempty"`
	UserID      string   `json:"userid"`
	CreatorName string   `json:"creator_name"`
	UseCount    int64    `json:"use_count"`
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
	CreatorName string   `json:"creator_name"`
	Timestamp   string   `json:"timestamp"`
	CreatedAt   string   `json:"created_at"`
	UseCount    int64    `json:"use_count"`
	LastUsedAt  string   `json:"last_used_at"`
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

// UpdateMemoryRequest 描述后台记忆编辑入参，仅开放可安全重算向量的正文相关字段。
type UpdateMemoryRequest struct {
	Title   string   `json:"title"`
	Tags    []string `json:"tags"`
	Summary string   `json:"summary"`
	Content string   `json:"content"`
}

// ProjectNameListResponse 返回可供下拉选择的项目名列表，避免前端自行扫描分页数据凑选项。
type ProjectNameListResponse struct {
	Items []string `json:"items"`
	Error string   `json:"error,omitempty"`
}

// ProjectTagListResponse 返回指定项目下的标签列表，避免前端从分页结果里拼凑不完整标签集。
type ProjectTagListResponse struct {
	ProjectName string   `json:"project_name"`
	Items       []string `json:"items"`
	Error       string   `json:"error,omitempty"`
}

// MergeProjectRequest 描述项目记忆合并请求，要求把副项目全部并入主项目。
type MergeProjectRequest struct {
	SourceProjectName string `json:"source_project_name"`
	TargetProjectName string `json:"target_project_name"`
}

// MergeProjectResponse 返回项目合并结果摘要，便于前端展示本次迁移规模。
type MergeProjectResponse struct {
	SourceProjectName              string `json:"source_project_name"`
	TargetProjectName              string `json:"target_project_name"`
	BatchCount                     int    `json:"batch_count"`
	MergedMemoryCount              int    `json:"merged_memory_count"`
	RebuiltEmbeddingCount          int    `json:"rebuilt_embedding_count"`
	ClearedReviewCount             int64  `json:"cleared_review_count"`
	InvalidatedApprovedReviewCount int64  `json:"invalidated_approved_review_count"`
	Message                        string `json:"message,omitempty"`
	Error                          string `json:"error,omitempty"`
}

// MergeTagsRequest 描述项目内标签合并请求，要求把多个副标签并入同一个主标签。
type MergeTagsRequest struct {
	ProjectName string   `json:"project_name"`
	SourceTags  []string `json:"source_tags"`
	TargetTag   string   `json:"target_tag"`
}

// MergeTagsResponse 返回标签合并结果摘要，便于前端解释影响范围与后续清理动作。
type MergeTagsResponse struct {
	ProjectName                    string   `json:"project_name"`
	SourceTags                     []string `json:"source_tags"`
	TargetTag                      string   `json:"target_tag"`
	BatchCount                     int      `json:"batch_count"`
	AffectedMemoryCount            int      `json:"affected_memory_count"`
	RebuiltEmbeddingCount          int      `json:"rebuilt_embedding_count"`
	ClearedPendingReviewCount      int64    `json:"cleared_pending_review_count"`
	InvalidatedApprovedReviewCount int64    `json:"invalidated_approved_review_count"`
	Message                        string   `json:"message,omitempty"`
	Error                          string   `json:"error,omitempty"`
}

// ChangePasswordRequest 描述用户修改密码的请求参数，允许用户更新自己的登录密码。
type ChangePasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// DashboardTagStat 描述热门标签统计项，便于前端按标签样式展示 Top 列表。
type DashboardTagStat struct {
	Tag   string `json:"tag"`
	Count int64  `json:"count"`
}

// DashboardMemoryTypeStat 描述记忆类型分布，避免前端重复计算 summary/error 口径。
type DashboardMemoryTypeStat struct {
	Summary int64 `json:"summary"`
	Error   int64 `json:"error"`
}

// DashboardBaseStats 描述总览页默认展示的基础统计信息。
type DashboardBaseStats struct {
	UserTotal      int64                   `json:"user_total"`
	MemoryTotal    int64                   `json:"memory_total"`
	MemoryType     DashboardMemoryTypeStat `json:"memory_type"`
	HotTags        []DashboardTagStat      `json:"hot_tags"`
	HotTagTopLimit int                     `json:"hot_tag_top_limit"`
}

// DashboardCacheStats 描述缓存统计信息，供缓存开启场景下展示运行状态。
type DashboardCacheStats struct {
	Enabled                  bool    `json:"enabled"`
	QueryEmbeddingEntryCount int     `json:"query_embedding_entry_count"`
	SemanticHitsEntryCount   int     `json:"semantic_hits_entry_count"`
	HitRate                  float64 `json:"hit_rate"`
	EstimatedMemoryBytes     int64   `json:"estimated_memory_bytes"`
	EstimatedMemoryHuman     string  `json:"estimated_memory_human"`
	QueryEmbeddingHitCount   uint64  `json:"query_embedding_hit_count"`
	QueryEmbeddingMissCount  uint64  `json:"query_embedding_miss_count"`
	SemanticHitsHitCount     uint64  `json:"semantic_hits_hit_count"`
	SemanticHitsMissCount    uint64  `json:"semantic_hits_miss_count"`
	QueryEmbeddingEvictCount uint64  `json:"query_embedding_evict_count"`
	SemanticHitsEvictCount   uint64  `json:"semantic_hits_evict_count"`
}

// DashboardStatsResponse 描述总览统计接口响应，避免前端拼接多个接口。
type DashboardStatsResponse struct {
	Base                     DashboardBaseStats  `json:"base"`
	Cache                    DashboardCacheStats `json:"cache"`
	CacheRefreshIntervalSecs int                 `json:"cache_refresh_interval_seconds"`
	Error                    string              `json:"error,omitempty"`
}

// DashboardCacheStatsResponse 描述缓存统计接口响应，便于前端按配置频率轮询。
type DashboardCacheStatsResponse struct {
	Cache DashboardCacheStats `json:"cache"`
	Error string              `json:"error,omitempty"`
}

// ProtectedTagItem 描述后台可维护的保护标签，避免清理规则只能通过本地配置调整。
type ProtectedTagItem struct {
	ID          int64  `json:"id"`
	Tag         string `json:"tag"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ProtectedTagListResponse 返回保护标签列表，便于管理页统一加载当前生效规则。
type ProtectedTagListResponse struct {
	Items []ProtectedTagItem `json:"items"`
	Error string             `json:"error,omitempty"`
}

// ProtectedTagMutationRequest 描述保护标签的新增与编辑入参，允许管理员在线维护清理白名单。
type ProtectedTagMutationRequest struct {
	Tag         string `json:"tag"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

// ProtectedTagMutationResponse 统一返回保护标签写操作结果，避免前端重复拼装对象。
type ProtectedTagMutationResponse struct {
	Item  ProtectedTagItem `json:"item,omitempty"`
	Error string           `json:"error,omitempty"`
}

// CleanupReviewItem 描述待清理候选快照，便于管理页解释每条候选的评分依据。
type CleanupReviewItem struct {
	ID            int64          `json:"id"`
	MemoryID      int64          `json:"memory_id"`
	ProjectName   string         `json:"project_name"`
	Type          string         `json:"type"`
	Status        string         `json:"status"`
	Score         float64        `json:"score"`
	Reason        map[string]any `json:"reason"`
	Snapshot      map[string]any `json:"snapshot"`
	RunAt         string         `json:"run_at"`
	ReviewedBy    string         `json:"reviewed_by"`
	ReviewedAt    string         `json:"reviewed_at"`
	ExecutionNote string         `json:"execution_note"`
	CreatedAt     string         `json:"created_at"`
}

// CleanupReviewListRequest 描述审核列表查询参数，避免前端自行约定筛选字段名。
type CleanupReviewListRequest struct {
	Page        int    `form:"page"`
	PageSize    int    `form:"page_size"`
	Status      string `form:"status"`
	Type        string `form:"type"`
	ProjectName string `form:"project_name"`
}

// CleanupReviewListResponse 统一返回审核分页结果，降低前端分页与筛选实现复杂度。
type CleanupReviewListResponse struct {
	Items     []CleanupReviewItem `json:"items"`
	Total     int64               `json:"total"`
	Page      int                 `json:"page"`
	PageSize  int                 `json:"page_size"`
	TotalPage int                 `json:"total_page"`
	Error     string              `json:"error,omitempty"`
}

// CleanupReviewActionRequest 描述批量审核或执行操作的目标记录集合。
type CleanupReviewActionRequest struct {
	IDs []int64 `json:"ids"`
}

// CleanupReviewRunResponse 返回一次候选生成或执行摘要，便于管理页提示管理员本轮结果。
type CleanupReviewRunResponse struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
