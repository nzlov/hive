package memory

import (
	"encoding/json"
	"time"

	"github.com/nzlov/hive/internal/models"
)

// Location 描述当前服务使用的记忆目录，避免数据库路径决策散落在多个层次。
type Location struct {
	// MemoryRoot 指向记忆数据库所在根目录，便于统一推导存储路径。
	MemoryRoot string
}

// Row 统一描述数据库里的记忆记录，避免搜索和写入层重复维护字段。
type Row struct {
	// ID 对应记忆主键，便于搜索结果与持久化记录互相映射。
	ID int64
	// UserID 记录记忆创建人，便于后续审计和管理端展示来源。
	UserID string
	// ProjectName 标识记忆所属项目，保证单库模式下的数据隔离语义稳定。
	ProjectName string
	// GitBranch 保留写入时分支信息，便于检索时判断上下文适用范围。
	GitBranch string
	// Type 区分总结与错误记忆，便于复用统一结构承接不同来源结果。
	Type string
	// Title 保存记忆标题，便于命中列表快速概览主题。
	Title string
	// Tags 保存标签 JSON 原文，供搜索层按需解码复用。
	Tags string
	// Summary 保存精简摘要，便于在不展开正文时也能判断内容价值。
	Summary string
	// Content 保存完整正文，供片段提取和语义召回直接使用。
	Content string
	// Timestamp 保存业务时间戳，保证排序和衰减逻辑口径一致。
	Timestamp string
	// CreatedAt 保存落库时间，便于后台审计和构造清理快照。
	CreatedAt string
}

// Hit 描述单条命中结果，让关键字检索和语义召回共享同一渲染模型。
type Hit struct {
	// ID 指向命中的记忆主键，便于后续累计使用次数或回表查询。
	ID int64
	// Source 标记结果来自 summary 还是 error 记忆，便于分组展示。
	Source string
	// GitBranch 保留命中记忆所在分支，帮助调用方判断是否需要过滤。
	GitBranch string
	// Title 保存命中标题，避免片段结果脱离主题难以理解。
	Title string
	// Tags 保存命中的标签列表，便于前端和脚本补充上下文判断。
	Tags []string
	// Timestamp 保存命中记忆时间，用于排序和结果展示。
	Timestamp time.Time
	// Confidence 保存统一归一化后的置信度，便于不同来源结果混排。
	Confidence float64
	// Snippets 保存正文命中片段，便于优先展示局部证据。
	Snippets []Snippet
	// FileContent 保存完整正文，在无法截取片段时仍能返回完整上下文。
	FileContent string
}

// Snippet 描述命中的片段及其行号范围，便于输出稳定的 Markdown 结构。
type Snippet struct {
	// Start 表示片段起始行号，便于调用方定位原始正文位置。
	Start int
	// End 表示片段结束行号，保证片段范围在输出中可追踪。
	End int
	// Content 保存片段正文，便于直接渲染到 Markdown 结果里。
	Content string
}

// RebuildResult 统一表达向量重建结果，方便 HTTP 接口和内部逻辑复用。
type RebuildResult struct {
	// Changed 表示本次是否真的执行了重建，避免调用方误判状态。
	Changed bool
	// Message 保存可读结果说明，便于日志和接口响应直接复用。
	Message string
}

// SearchResult 统一承接搜索输出，避免服务端和脚本各自维护一套结果结构。
type SearchResult struct {
	// Query 保存原始查询串，便于结果回显和调试输出复用。
	Query string
	// ProjectName 保存当前查询项目名，便于多项目场景下回显隔离维度。
	ProjectName string
	// DebugCommands 记录调试命令描述，便于问题排查时复现实验路径。
	DebugCommands []string
	// ErrorHits 保存错误记忆命中结果，便于按类型分组输出。
	ErrorHits []Hit
	// SummaryHits 保存总结记忆命中结果，避免不同类型结果混在一起。
	SummaryHits []Hit
}

// MemoryListItem 统一承接列表项和可选置信度，避免管理端与搜索端各自维护映射规则。
type MemoryListItem struct {
	// Memory 保存完整记忆模型，便于管理端直接复用字段渲染。
	Memory models.Memory
	// Confidence 在搜索模式下附带命中分，普通列表模式下允许为空。
	Confidence *float64
}

// MemoryListResult 描述管理端记忆列表分页结果，便于 HTTP 层复用统一查询逻辑。
type MemoryListResult struct {
	// Items 保存当前页列表项，兼容普通浏览与搜索结果两种模式。
	Items []MemoryListItem
	// Total 保存符合条件的总记录数，便于前端计算分页器状态。
	Total int64
	// Page 保存当前页码，避免响应层再自行回填输入参数。
	Page int
	// PageSize 保存当前页大小，便于前端维持查询状态。
	PageSize int
	// TotalPage 保存总页数，避免调用方重复实现分页计算逻辑。
	TotalPage int
}

// CacheStatsSnapshot 描述缓存运行时快照，便于管理端观察命中率与容量占用。
type CacheStatsSnapshot struct {
	// Enabled 标记当前是否启用缓存，便于页面决定是否展示指标。
	Enabled bool
	// QueryEmbeddingEntryCount 统计查询向量缓存项数量，便于观察缓存规模。
	QueryEmbeddingEntryCount int
	// SemanticHitsEntryCount 统计语义结果缓存项数量，便于分开观察两类缓存。
	SemanticHitsEntryCount int
	// QueryEmbeddingHitCount 统计查询向量缓存命中次数，便于衡量缓存收益。
	QueryEmbeddingHitCount uint64
	// QueryEmbeddingMissCount 统计查询向量缓存未命中次数，便于分析抖动来源。
	QueryEmbeddingMissCount uint64
	// SemanticHitsHitCount 统计语义命中缓存命中次数，便于判断重复查询复用率。
	SemanticHitsHitCount uint64
	// SemanticHitsMissCount 统计语义命中缓存未命中次数，便于观测冷启动成本。
	SemanticHitsMissCount uint64
	// QueryEmbeddingEvictCount 统计查询向量缓存淘汰次数，便于评估容量是否偏小。
	QueryEmbeddingEvictCount uint64
	// SemanticHitsEvictCount 统计语义结果缓存淘汰次数，便于评估容量压力。
	SemanticHitsEvictCount uint64
	// EstimatedMemoryBytes 粗略估算缓存占用内存，帮助观察内存增长趋势。
	EstimatedMemoryBytes int64
	// HitRate 保存整体缓存命中率，便于页面直接展示核心指标。
	HitRate float64
}

// CleanupRunResult 描述一次清理候选生成结果，便于 cron 日志和管理端提示复用同一摘要。
type CleanupRunResult struct {
	// RunAt 保存本轮清理时间标识，便于关联审核记录和日志。
	RunAt string
	// Mode 保存当前清理模式，便于区分 review 与 auto 两种执行路径。
	Mode string
	// SummaryCandidateCount 统计总结记忆候选数量，便于快速判断本轮规模。
	SummaryCandidateCount int
	// ErrorCandidateCount 统计错误记忆候选数量，便于区分不同类型压力。
	ErrorCandidateCount int
	// Message 保存可读摘要，便于接口和日志复用同一结果说明。
	Message string
}

// CleanupExecuteResult 描述一次审核执行结果，避免管理端只能从字符串里反推执行数量。
type CleanupExecuteResult struct {
	// ExecutedReviewCount 保存本次处理的审核记录数，便于后台展示执行规模。
	ExecutedReviewCount int
	// ExecutedMemoryCount 保存实际删除的记忆数量，便于区分审核数和删除数。
	ExecutedMemoryCount int
	// Message 保存可读执行结果，便于接口响应直接展示。
	Message string
}

// CleanupScoreDetail 保存清理评分拆解明细，便于后台解释候选为何进入待审核队列。
type CleanupScoreDetail struct {
	// AgeDays 记录记忆已存在天数，便于解释时间衰减部分得分。
	AgeDays int `json:"age_days"`
	// UseCount 记录记忆累计命中次数，便于说明为何被视为低价值候选。
	UseCount int64 `json:"use_count"`
	// LastUsedDays 记录距最近使用的天数，便于解释冷数据惩罚。
	LastUsedDays int `json:"last_used_days"`
	// ProjectName 保存候选所属项目，便于后台结合项目压力理解评分。
	ProjectName string `json:"project_name"`
	// ProjectTypeCount 保存该项目下同类型记忆总数，便于解释项目压力分。
	ProjectTypeCount int64 `json:"project_type_count"`
	// ProtectedTagHit 标记是否命中过保护标签，便于说明候选被跳过原因。
	ProtectedTagHit bool `json:"protected_tag_hit"`
	// Total 保存最终总分，便于后台直接排序展示。
	Total float64 `json:"total"`
	// SubScores 保存各分项得分，便于解释整体评分拆解。
	SubScores map[string]float64 `json:"sub_scores"`
}

// ToJSON 把评分详情序列化为稳定 JSON，避免审核表直接依赖 map 的默认字符串格式。
func (d CleanupScoreDetail) ToJSON() string {
	data, _ := json.Marshal(d)
	return string(data)
}
