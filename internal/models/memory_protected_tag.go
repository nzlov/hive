package models

import (
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// MemoryProtectedTag 保存清理保护标签，避免仅依赖本地配置导致管理员调整无法在线生效。
type MemoryProtectedTag struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Tag         string `gorm:"column:tag;type:text;not null;uniqueIndex"`
	Enabled     bool   `gorm:"column:enabled;not null;default:true"`
	Description string `gorm:"column:description;type:text;not null;default:''"`
	Source      string `gorm:"column:source;type:text;not null;default:'manual'"`
	CreatedAt   string `gorm:"column:created_at;type:text;not null"`
	UpdatedAt   string `gorm:"column:updated_at;type:text;not null"`
}

// TableName 固定表名，避免保护标签在不同数据库后端下出现命名漂移。
func (MemoryProtectedTag) TableName() string {
	return "memory_protected_tags"
}

// ListEnabledProtectedTags 返回当前启用的保护标签，供清理流程快速判断是否跳过候选。
func (s *Store) ListEnabledProtectedTags() ([]MemoryProtectedTag, error) {
	var items []MemoryProtectedTag
	err := s.db.Where("enabled = ?", true).Order("tag ASC").Find(&items).Error
	return items, err
}

// ListProtectedTags 返回全部保护标签列表，便于后台管理页统一展示开关和来源信息。
func (s *Store) ListProtectedTags() ([]MemoryProtectedTag, error) {
	var items []MemoryProtectedTag
	err := s.db.Order("tag ASC").Find(&items).Error
	return items, err
}

// CreateProtectedTag 创建保护标签，保证人工维护的规则能持久化到数据库。
func (s *Store) CreateProtectedTag(item MemoryProtectedTag) (MemoryProtectedTag, error) {
	item.Tag = strings.TrimSpace(item.Tag)
	if item.CreatedAt == "" {
		item.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.UpdatedAt == "" {
		item.UpdatedAt = item.CreatedAt
	}
	return item, s.db.Create(&item).Error
}

// UpdateProtectedTag 更新保护标签的可维护字段，避免后台修改误覆盖创建元数据。
func (s *Store) UpdateProtectedTag(id int64, item MemoryProtectedTag) (MemoryProtectedTag, error) {
	updates := map[string]any{
		"tag":         strings.TrimSpace(item.Tag),
		"enabled":     item.Enabled,
		"description": strings.TrimSpace(item.Description),
		"source":      strings.TrimSpace(item.Source),
		"updated_at":  time.Now().UTC().Format(time.RFC3339Nano),
	}
	result := s.db.Model(&MemoryProtectedTag{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return MemoryProtectedTag{}, result.Error
	}
	if result.RowsAffected == 0 {
		return MemoryProtectedTag{}, ErrNotFound
	}
	var out MemoryProtectedTag
	err := normalizeNotFound(s.db.Where("id = ?", id).First(&out).Error)
	return out, err
}

// DeleteProtectedTag 删除保护标签，供管理员清理失效规则。
func (s *Store) DeleteProtectedTag(id int64) error {
	return s.db.Delete(&MemoryProtectedTag{}, id).Error
}

// UpsertProtectedTags 用配置默认值补种保护标签，避免重启后覆盖管理员已有调整。
func (s *Store) UpsertProtectedTags(items []MemoryProtectedTag) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tag"}},
		DoNothing: true,
	}).Create(&items).Error
}
