package models

import "gorm.io/gorm/clause"

// MemoryMetadata 对应 memory_metadata 表，保存跨流程共享的数据库元信息。
type MemoryMetadata struct {
	Key       string `gorm:"column:key;primaryKey;type:text"`
	Value     string `gorm:"column:value;type:text;not null"`
	UpdatedAt string `gorm:"column:updated_at;type:text;not null"`
}

// TableName 固定表名，避免自动命名破坏现有元数据读取路径。
func (MemoryMetadata) TableName() string {
	return "memory_metadata"
}

// GetMemoryMetadata 统一读取元数据，减少业务层重复处理空记录分支。
func (s *Store) GetMemoryMetadata(key string) (string, error) {
	var item MemoryMetadata
	err := s.db.Where("key = ?", key).First(&item).Error
	if err != nil {
		if normalizeNotFound(err) == ErrNotFound {
			return "", nil
		}
		return "", err
	}
	return item.Value, nil
}

// SetMemoryMetadata 使用统一的 upsert 逻辑写入元数据，确保模型切换状态单点维护。
func (s *Store) SetMemoryMetadata(key, value, updatedAt string) error {
	item := MemoryMetadata{Key: key, Value: value, UpdatedAt: updatedAt}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&item).Error
}
