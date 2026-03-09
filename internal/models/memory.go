package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Memory 对应 memories 表，集中维护记忆记录的结构和索引声明。
type Memory struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      string `gorm:"column:userid;type:text;not null;default:''"`
	ProjectName string `gorm:"column:project_name;type:text;not null;default:'';index:idx_memories_project_type_timestamp,priority:1"`
	GitBranch   string `gorm:"column:git_branch;type:text;not null;default:'';index:idx_memories_project_type_timestamp,priority:4"`
	Type        string `gorm:"column:type;type:text;not null;check:type IN ('summary','error');index:idx_memories_type_timestamp,priority:1;index:idx_memories_project_type_timestamp,priority:2"`
	Title       string `gorm:"column:title;type:text;not null"`
	Tags        string `gorm:"column:tags;type:text;not null;default:'[]'"`
	Summary     string `gorm:"column:summary;type:text;not null;default:''"`
	Content     string `gorm:"column:content;type:text;not null"`
	Timestamp   string `gorm:"column:timestamp;type:text;not null;index:idx_memories_type_timestamp,priority:2;index:idx_memories_project_type_timestamp,priority:5"`
	CreatedAt   string `gorm:"column:created_at;type:text;not null"`
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

// ListMemoriesByProjectAndType 按项目和类型筛选记忆，维持单库模式下的项目隔离。
func (s *Store) ListMemoriesByProjectAndType(projectName, memType string) ([]Memory, error) {
	var items []Memory
	err := s.db.Where("project_name = ? AND type = ?", strings.TrimSpace(projectName), strings.TrimSpace(memType)).Order("timestamp DESC").Order("id DESC").Find(&items).Error
	return items, err
}

// SearchMemoriesByProjectAndType 使用 SQL 关键字过滤候选记忆，避免先全量取回再在业务层粗筛。
func (s *Store) SearchMemoriesByProjectAndType(projectName, memType string, queries []string) ([]Memory, error) {
	cleaned := normalizeKeywordQueries(queries)
	if len(cleaned) == 0 {
		return []Memory{}, nil
	}
	db := s.db.Where("project_name = ? AND type = ?", strings.TrimSpace(projectName), strings.TrimSpace(memType))
	for _, query := range cleaned {
		likeValue := "%" + escapeLike(query) + "%"
		db = db.Where(buildKeywordWhereClause(), likeValue, likeValue, likeValue, likeValue, likeValue)
	}
	var items []Memory
	err := db.Order("timestamp DESC").Order("id DESC").Find(&items).Error
	return items, err
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
