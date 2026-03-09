package models

import (
	"encoding/json"

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
	ProjectName string `gorm:"column:project_name;type:text;not null;default:'';index:idx_memory_embeddings_project_memory,priority:1"`
	Vector      string `gorm:"column:vector;type:text;not null"`
	UpdatedAt   string `gorm:"column:updated_at;type:text;not null"`
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
		DoUpdates: clause.AssignmentColumns([]string{"project_name", "vector", "updated_at"}),
	}).Create(&items).Error
}

// DeleteAllMemoryEmbeddings 在全量重建前清空旧向量，避免新旧维度混用。
func (s *Store) DeleteAllMemoryEmbeddings() error {
	return s.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&MemoryEmbedding{}).Error
}

// CountMemoryEmbeddings 返回向量总数，供启动时快速判断是否需要重建。
func (s *Store) CountMemoryEmbeddings() (int64, error) {
	var total int64
	err := s.db.Model(&MemoryEmbedding{}).Count(&total).Error
	return total, err
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

// ListSemanticEmbeddingCandidates 分页读取指定项目和类型的向量候选，让服务层按批打分而不是一次性全量载入。
func (s *Store) ListSemanticEmbeddingCandidates(projectName, memType string, limit, offset int) ([]MemoryEmbeddingCandidate, error) {
	if limit <= 0 {
		return []MemoryEmbeddingCandidate{}, nil
	}
	var items []MemoryEmbeddingCandidate
	err := s.db.Table("memory_embeddings AS me").
		Select("me.memory_id, me.vector, m.timestamp").
		Joins("JOIN memories AS m ON m.id = me.memory_id").
		Where("me.project_name = ? AND m.project_name = ? AND m.type = ?", projectName, projectName, memType).
		Order("m.timestamp DESC").
		Order("me.memory_id DESC").
		Limit(limit).
		Offset(offset).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
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
