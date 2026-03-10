package models

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
)

// Memory 对应 memories 表，集中维护记忆记录的结构和索引声明。
type Memory struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement;comment:记忆记录自增主键"`
	UserID      string `gorm:"column:userid;type:text;not null;default:'';comment:创建记忆的业务用户标识"`
	ProjectName string `gorm:"column:project_name;type:text;not null;default:'';index:idx_memories_project_type_timestamp,priority:1;comment:记忆所属项目名"`
	GitBranch   string `gorm:"column:git_branch;type:text;not null;default:'';index:idx_memories_project_type_timestamp,priority:4;comment:记忆写入时所在分支"`
	Type        string `gorm:"column:type;type:text;not null;check:type IN ('summary','error');index:idx_memories_type_timestamp,priority:1;index:idx_memories_project_type_timestamp,priority:2;comment:记忆类型"`
	Title       string `gorm:"column:title;type:text;not null;comment:记忆标题"`
	Tags        string `gorm:"column:tags;type:text;not null;default:'[]';comment:记忆标签 JSON"`
	Summary     string `gorm:"column:summary;type:text;not null;default:'';comment:记忆摘要"`
	Content     string `gorm:"column:content;type:text;not null;comment:记忆正文"`
	UseCount    int64  `gorm:"column:use_count;not null;default:0;comment:记忆命中次数"`
	LastUsedAt  string `gorm:"column:last_used_at;type:text;not null;default:'';comment:最近命中时间"`
	Timestamp   string `gorm:"column:timestamp;type:text;not null;index:idx_memories_type_timestamp,priority:2;index:idx_memories_project_type_timestamp,priority:5;comment:记忆业务时间戳"`
	CreatedAt   string `gorm:"column:created_at;type:text;not null;comment:记忆创建时间"`
}

// MemoryLite 只保留搜索回表阶段需要的字段，避免候选筛选前过早搬运大正文。
type MemoryLite struct {
	ID        int64  `gorm:"column:id;comment:记忆主键"`
	GitBranch string `gorm:"column:git_branch;comment:记忆所在分支"`
	Title     string `gorm:"column:title;comment:记忆标题"`
	Tags      string `gorm:"column:tags;comment:记忆标签 JSON"`
	Summary   string `gorm:"column:summary;comment:记忆摘要"`
	Content   string `gorm:"column:content;comment:记忆正文"`
	Timestamp string `gorm:"column:timestamp;comment:记忆业务时间戳"`
}

// TableName 固定表名，避免 GORM 复数化规则影响既有数据表兼容性。
func (Memory) TableName() string {
	return "memories"
}

// CreateMemories 批量写入记忆记录，确保服务层不再直接拼接 INSERT 语句。
func (s *Store) CreateMemories(items []Memory) ([]Memory, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if err := s.db.Create(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListAllMemories 读取全部记忆，供向量重建等全量流程复用统一查询入口。
func (s *Store) ListAllMemories() ([]Memory, error) {
	var items []Memory
	err := s.db.Order("timestamp DESC").Order("id DESC").Find(&items).Error
	return items, err
}

// GetMemoryByID 读取单条记忆详情，避免管理端为查看详情自行拼接查询条件。
func (s *Store) GetMemoryByID(id int64) (Memory, error) {
	var item Memory
	err := normalizeNotFound(s.db.Where("id = ?", id).First(&item).Error)
	return item, err
}

// SaveMemoryEditableFields 仅更新允许后台编辑的记忆字段，避免不可编辑元数据被误覆盖。
func (s *Store) SaveMemoryEditableFields(item Memory) (Memory, error) {
	result := s.db.Model(&Memory{}).Where("id = ?", item.ID).Updates(map[string]any{
		"title":   item.Title,
		"tags":    item.Tags,
		"summary": item.Summary,
		"content": item.Content,
	})
	if result.Error != nil {
		return Memory{}, result.Error
	}
	if result.RowsAffected == 0 {
		return Memory{}, ErrNotFound
	}
	return s.GetMemoryByID(item.ID)
}

// DeleteMemoryByID 删除单条记忆，供管理端和后续清理流程复用同一持久化入口。
func (s *Store) DeleteMemoryByID(id int64) error {
	return s.db.Delete(&Memory{}, id).Error
}

// DeleteMemoriesByIDs 批量删除记忆主记录，避免清理任务逐条删除放大事务成本。
func (s *Store) DeleteMemoriesByIDs(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Delete(&Memory{}, "id IN ?", ids).Error
}

// IncrementMemoryUseCounts 为最终返回给调用方的记忆批量累计使用次数并刷新最近使用时间。
func (s *Store) IncrementMemoryUseCounts(ids []int64, usedAt string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Model(&Memory{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"use_count":    gorm.Expr("use_count + ?", 1),
			"last_used_at": strings.TrimSpace(usedAt),
		}).Error
}

// CountMemoriesByType 返回指定类型的记忆总数，供清理流程应用全局保底阈值。
func (s *Store) CountMemoriesByType(memType string) (int64, error) {
	var total int64
	err := s.db.Model(&Memory{}).Where("type = ?", strings.TrimSpace(memType)).Count(&total).Error
	return total, err
}

// CountMemoriesByProjectAndType 返回指定项目和类型的记忆总数，供清理流程应用项目保底阈值。
func (s *Store) CountMemoriesByProjectAndType(projectName, memType string) (int64, error) {
	var total int64
	err := s.db.Model(&Memory{}).
		Where("project_name = ? AND type = ?", strings.TrimSpace(projectName), strings.TrimSpace(memType)).
		Count(&total).Error
	return total, err
}

// ListMemoryCleanupCandidates 返回超过候选年龄的粗筛记忆，细粒度评分和保护规则交由服务层统一处理。
func (s *Store) ListMemoryCleanupCandidates(memType, cutoffTimestamp string, limit int) ([]Memory, error) {
	if limit <= 0 {
		return []Memory{}, nil
	}
	var items []Memory
	err := s.db.Where("type = ? AND timestamp < ?", strings.TrimSpace(memType), strings.TrimSpace(cutoffTimestamp)).
		Order("timestamp ASC").
		Order("id ASC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// ListMemoriesPaginated 提供后台管理的分页列表，复用统一关键字搜索逻辑避免筛选口径不一致。
func (s *Store) ListMemoriesPaginated(page, pageSize int, queries []string) ([]Memory, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	pageSize = int(math.Min(float64(pageSize), 100))

	db := applyMemoryKeywordFilters(s.db.Model(&Memory{}), queries)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Memory
	err := db.Order("timestamp DESC").Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// ListMemoriesByProjectAndType 按项目和类型筛选记忆，维持单库模式下的项目隔离。
func (s *Store) ListMemoriesByProjectAndType(projectName, memType string) ([]Memory, error) {
	var items []Memory
	err := s.db.Where("project_name = ? AND type = ?", strings.TrimSpace(projectName), strings.TrimSpace(memType)).Order("timestamp DESC").Order("id DESC").Find(&items).Error
	return items, err
}

// ListMemoriesByIDs 批量读取指定主键的记忆，避免管理列表按命中顺序逐条回表放大查询次数。
func (s *Store) ListMemoriesByIDs(memoryIDs []int64) ([]Memory, error) {
	if len(memoryIDs) == 0 {
		return []Memory{}, nil
	}
	var items []Memory
	err := s.db.Where("id IN ?", memoryIDs).Find(&items).Error
	return items, err
}

// ListMemoryLitesByProjectTypeAndIDs 在候选打分后再回表取正文，允许按项目隔离或跨项目查询同一类型候选。
func (s *Store) ListMemoryLitesByProjectTypeAndIDs(projectName, memType string, memoryIDs []int64) ([]MemoryLite, error) {
	if len(memoryIDs) == 0 {
		return []MemoryLite{}, nil
	}
	var items []MemoryLite
	db := s.db.Model(&Memory{}).
		Select("id, git_branch, title, tags, summary, content, timestamp").
		Where("type = ? AND id IN ?", strings.TrimSpace(memType), memoryIDs)
	if cleanedProjectName := strings.TrimSpace(projectName); cleanedProjectName != "" {
		db = db.Where("project_name = ?", cleanedProjectName)
	}
	err := db.Find(&items).Error
	return items, err
}

// SearchMemoriesByProjectAndType 使用 SQL 关键字过滤候选记忆，允许按项目隔离或跨项目复用同一筛选逻辑。
func (s *Store) SearchMemoriesByProjectAndType(projectName, memType string, queries []string) ([]Memory, error) {
	cleaned := normalizeKeywordQueries(queries)
	if len(cleaned) == 0 {
		return []Memory{}, nil
	}
	db := s.db.Where("type = ?", strings.TrimSpace(memType))
	if cleanedProjectName := strings.TrimSpace(projectName); cleanedProjectName != "" {
		db = db.Where("project_name = ?", cleanedProjectName)
	}
	db = applyMemoryKeywordFilters(db, cleaned)
	var items []Memory
	err := db.Order("timestamp DESC").Order("id DESC").Find(&items).Error
	return items, err
}

// applyMemoryKeywordFilters 统一关键字筛选条件，避免搜索记忆和管理列表在字段口径上分叉。
func applyMemoryKeywordFilters(db *gorm.DB, queries []string) *gorm.DB {
	cleaned := normalizeKeywordQueries(queries)
	for _, query := range cleaned {
		likeValue := "%" + escapeLike(query) + "%"
		db = db.Where(buildKeywordWhereClause(), likeValue, likeValue, likeValue, likeValue, likeValue)
	}
	return db
}

// buildKeywordWhereClause 统一描述关键字搜索条件，避免 SQLite 和 PostgreSQL 下查询字段漂移。
func buildKeywordWhereClause() string {
	return strings.Join([]string{
		"(",
		"LOWER(title) LIKE LOWER(?) ESCAPE '\\'",
		"OR LOWER(summary) LIKE LOWER(?) ESCAPE '\\'",
		"OR LOWER(tags) LIKE LOWER(?) ESCAPE '\\'",
		"OR LOWER(content) LIKE LOWER(?) ESCAPE '\\'",
		"OR LOWER(project_name) LIKE LOWER(?) ESCAPE '\\'",
		")",
	}, " ")
}

// normalizeKeywordQueries 统一裁剪空查询，避免生成无意义的 SQL 条件。
func normalizeKeywordQueries(queries []string) []string {
	out := make([]string, 0, len(queries))
	for _, query := range queries {
		if cleaned := strings.TrimSpace(query); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

// escapeLike 转义 LIKE 通配符，避免用户关键字中的特殊字符改变匹配语义。
func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}

// EncodeTags 使用 JSON 序列化标签，避免标签内容被自定义分隔符污染。
func EncodeTags(tags []string) string {
	data, _ := json.Marshal(tags)
	return string(data)
}

// DecodeTags 宽松解析标签，避免单条脏数据让整个检索流程失败。
func DecodeTags(raw string) []string {
	var payload []any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	out := make([]string, 0, len(payload))
	for _, item := range payload {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}
