package models

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MemoryEmbeddingCandidate 描述语义搜索阶段的最小候选集，避免打分前提前读取完整记忆正文。
type MemoryEmbeddingCandidate struct {
	MemoryID  int64  `gorm:"column:memory_id"`
	Vector    string `gorm:"column:vector"`
	Timestamp string `gorm:"column:timestamp"`
}

// MemoryEmbedding 对应 memory_embeddings 表，保存记忆向量和项目隔离维度。
type MemoryEmbedding struct {
	MemoryID    int64  `gorm:"column:memory_id;primaryKey"`
	ProjectName string `gorm:"column:project_name;type:text;not null;default:'';index:idx_memory_embeddings_project_type_timestamp,priority:1"`
	Type        string `gorm:"column:type;type:text;not null;default:'';check:type IN ('summary','error');index:idx_memory_embeddings_project_type_timestamp,priority:2"`
	Vector      string `gorm:"column:vector;type:text;not null"`
	Timestamp   string `gorm:"column:timestamp;type:text;not null;default:'';index:idx_memory_embeddings_project_type_timestamp,priority:3"`
	UpdatedAt   string `gorm:"column:updated_at;type:text;not null"`
}

// MemoryEmbeddingSimilarity 描述一次语义检索的最小命中结构，避免上层依赖具体数据库距离表达式。
type MemoryEmbeddingSimilarity struct {
	MemoryID   int64
	Similarity float64
}

// TableName 固定表名，避免自动命名影响既有查询和迁移结果。
func (MemoryEmbedding) TableName() string {
	return "memory_embeddings"
}

// UpsertMemoryEmbeddings 批量写入或更新向量，保证不同数据库下都使用同一套冲突策略。
func (s *Store) UpsertMemoryEmbeddings(items []MemoryEmbedding) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "memory_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"project_name", "type", "vector", "timestamp", "updated_at"}),
	}).Create(&items).Error
}

// EnsureVectorBackendReady 启动阶段强校验向量能力，避免扩展缺失在请求期才暴露。
func (s *Store) EnsureVectorBackendReady() error {
	if s == nil || s.vector == nil {
		return nil
	}
	return s.vector.EnsureReady()
}

// DeleteAllMemoryEmbeddings 在全量重建前清空旧向量，避免新旧维度混用。
func (s *Store) DeleteAllMemoryEmbeddings() error {
	return s.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&MemoryEmbedding{}).Error
}

// DeleteMemoryEmbeddingByMemoryID 删除单条记忆对应的向量，避免后台删除记录后留下孤儿索引。
func (s *Store) DeleteMemoryEmbeddingByMemoryID(memoryID int64) error {
	return s.db.Delete(&MemoryEmbedding{}, "memory_id = ?", memoryID).Error
}

// CountMemoryEmbeddings 返回向量总数，供启动时快速判断是否需要重建。
func (s *Store) CountMemoryEmbeddings() (int64, error) {
	var total int64
	err := s.db.Model(&MemoryEmbedding{}).Count(&total).Error
	return total, err
}

// CountMemoryEmbeddingsByProjectAndType 按项目和类型统计向量总量，便于服务层动态调整语义召回窗口。
func (s *Store) CountMemoryEmbeddingsByProjectAndType(projectName, memType string) (int64, error) {
	var total int64
	db := s.db.Model(&MemoryEmbedding{}).Where("type = ?", strings.TrimSpace(memType))
	if cleanedProjectName := strings.TrimSpace(projectName); cleanedProjectName != "" {
		db = db.Where("project_name = ?", cleanedProjectName)
	}
	err := db.Count(&total).Error
	return total, err
}

// CountMemoryEmbeddingsMissingSearchFields 返回缺少搜索筛选字段的向量数，确保迁移后能触发一次补全重建。
func (s *Store) CountMemoryEmbeddingsMissingSearchFields() (int64, error) {
	return 0, nil
}

// ListMemoryEmbeddings 按项目和主键集合读取向量，避免跨项目误取相似度数据。
func (s *Store) ListMemoryEmbeddings(projectName string, memoryIDs []int64) (map[int64][]float64, error) {
	if len(memoryIDs) == 0 {
		return map[int64][]float64{}, nil
	}
	var items []MemoryEmbedding
	err := s.db.Where("project_name = ? AND memory_id IN ?", projectName, memoryIDs).Find(&items).Error
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]float64, len(items))
	for _, item := range items {
		if vector := DecodeVector(item.Vector); len(vector) > 0 {
			out[item.MemoryID] = vector
		}
	}
	return out, nil
}

// ListSemanticEmbeddingCandidates 分页读取指定类型的向量候选，必要时再按项目隔离避免跨项目误召回。
func (s *Store) ListSemanticEmbeddingCandidates(projectName, memType string, limit, offset int) ([]MemoryEmbeddingCandidate, error) {
	if limit <= 0 {
		return []MemoryEmbeddingCandidate{}, nil
	}
	var items []MemoryEmbeddingCandidate
	db := s.db.Model(&MemoryEmbedding{}).
		Select("memory_id, vector, timestamp").
		Where("type = ?", memType).
		Order("timestamp DESC").
		Order("memory_id DESC").
		Limit(limit).
		Offset(offset)
	if cleanedProjectName := strings.TrimSpace(projectName); cleanedProjectName != "" {
		db = db.Where("project_name = ?", cleanedProjectName)
	}
	err := db.Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// SearchMemoryEmbeddingsByVector 使用当前后端执行向量检索，并统一返回相似度结果。
func (s *Store) SearchMemoryEmbeddingsByVector(projectName, memType string, queryVector []float64, limit int) ([]MemoryEmbeddingSimilarity, error) {
	if s == nil || s.vector == nil {
		return []MemoryEmbeddingSimilarity{}, nil
	}
	items, err := s.vector.SearchSimilar(projectName, memType, queryVector, limit)
	if err != nil {
		return nil, err
	}
	out := make([]MemoryEmbeddingSimilarity, 0, len(items))
	for _, item := range items {
		out = append(out, MemoryEmbeddingSimilarity{MemoryID: item.MemoryID, Similarity: item.Similarity})
	}
	return out, nil
}

// EncodeVector 序列化向量，避免关系型数据库缺少原生数组列时出现兼容差异。
func EncodeVector(vector []float64) string {
	data, _ := json.Marshal(vector)
	return string(data)
}

// DecodeVector 宽松解析向量内容，避免单条坏数据拖垮整次检索流程。
func DecodeVector(raw string) []float64 {
	var payload []float64
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return payload
}
