package models

import (
	"strings"

	"gorm.io/gorm/clause"
)

// MemoryCleanupReview 保存一次清理任务生成的候选快照，避免审核过程因数据变化导致口径漂移。
type MemoryCleanupReview struct {
	ID            int64   `gorm:"column:id;primaryKey;autoIncrement;comment:审核记录自增主键"`
	MemoryID      int64   `gorm:"column:memory_id;not null;index:idx_cleanup_review_lookup,priority:1;comment:待清理记忆主键"`
	ProjectName   string  `gorm:"column:project_name;type:text;not null;default:'';index:idx_cleanup_review_lookup,priority:4;comment:候选所属项目名"`
	Type          string  `gorm:"column:type;type:text;not null;default:'';index:idx_cleanup_review_lookup,priority:3;comment:记忆类型"`
	Status        string  `gorm:"column:status;type:text;not null;default:'pending';index:idx_cleanup_review_lookup,priority:2;comment:审核状态"`
	Score         float64 `gorm:"column:score;not null;default:0;comment:清理评分"`
	ReasonJSON    string  `gorm:"column:reason_json;type:text;not null;default:'{}';comment:评分原因快照 JSON"`
	SnapshotJSON  string  `gorm:"column:snapshot_json;type:text;not null;default:'{}';comment:候选快照 JSON"`
	RunAt         string  `gorm:"column:run_at;type:text;not null;comment:清理任务轮次时间"`
	ReviewedBy    string  `gorm:"column:reviewed_by;type:text;not null;default:'';comment:审核人标识"`
	ReviewedAt    string  `gorm:"column:reviewed_at;type:text;not null;default:'';comment:审核时间"`
	ExecutionNote string  `gorm:"column:execution_note;type:text;not null;default:'';comment:执行备注"`
	CreatedAt     string  `gorm:"column:created_at;type:text;not null;comment:审核记录创建时间"`
}

// TableName 固定表名，避免审核记录跨环境迁移时出现命名不一致。
func (MemoryCleanupReview) TableName() string {
	return "memory_cleanup_reviews"
}

// ReplacePendingCleanupReviews 用最新结果替换同轮次的待审核候选，确保后台看到的是一致快照。
func (s *Store) ReplacePendingCleanupReviews(runAt, memType string, items []MemoryCleanupReview) error {
	return s.WithTx(func(txStore *Store) error {
		if err := txStore.db.Where("run_at = ? AND type = ? AND status = ?", strings.TrimSpace(runAt), strings.TrimSpace(memType), "pending").Delete(&MemoryCleanupReview{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		return txStore.db.Create(&items).Error
	})
}

// ListCleanupReviews 返回审核列表分页数据，避免后台页面直接拼接数据库查询条件。
func (s *Store) ListCleanupReviews(status, memType, projectName string, page, pageSize int) ([]MemoryCleanupReview, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	db := s.db.Model(&MemoryCleanupReview{})
	if value := strings.TrimSpace(status); value != "" {
		db = db.Where("status = ?", value)
	}
	if value := strings.TrimSpace(memType); value != "" {
		db = db.Where("type = ?", value)
	}
	if value := strings.TrimSpace(projectName); value != "" {
		db = db.Where("project_name = ?", value)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []MemoryCleanupReview
	err := db.Order("created_at DESC").Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// UpdateCleanupReviewStatus 批量更新审核状态，避免页面逐条提交产生额外事务开销。
func (s *Store) UpdateCleanupReviewStatus(ids []int64, status, reviewedBy, reviewedAt string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Model(&MemoryCleanupReview{}).Where("id IN ?", ids).Updates(map[string]any{
		"status":      strings.TrimSpace(status),
		"reviewed_by": strings.TrimSpace(reviewedBy),
		"reviewed_at": strings.TrimSpace(reviewedAt),
	}).Error
}

// ListApprovedCleanupReviews 返回待执行的审核通过记录，供后台手动执行或自动清理复用同一入口。
func (s *Store) ListApprovedCleanupReviews(limit int) ([]MemoryCleanupReview, error) {
	if limit <= 0 {
		return []MemoryCleanupReview{}, nil
	}
	var items []MemoryCleanupReview
	err := s.db.Where("status = ?", "approved").Order("score DESC").Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

// MarkCleanupReviewsExecuted 标记审核记录已执行，避免重复删除同一批记忆。
func (s *Store) MarkCleanupReviewsExecuted(ids []int64, reviewedBy, reviewedAt, note string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Model(&MemoryCleanupReview{}).Where("id IN ?", ids).Updates(map[string]any{
		"status":         "executed",
		"reviewed_by":    strings.TrimSpace(reviewedBy),
		"reviewed_at":    strings.TrimSpace(reviewedAt),
		"execution_note": strings.TrimSpace(note),
	}).Error
}

// DeleteCleanupReviewsByStatus 删除指定状态审核记录，避免 review 模式下历史待审记录无限累积。
func (s *Store) DeleteCleanupReviewsByStatus(status string) error {
	return s.db.Where("status = ?", strings.TrimSpace(status)).Delete(&MemoryCleanupReview{}).Error
}

// DeleteCleanupReviewsByProjectName 删除指定项目的全部审核记录，避免项目合并后旧项目快照继续干扰治理页。
func (s *Store) DeleteCleanupReviewsByProjectName(projectName string) (int64, error) {
	result := s.db.Where("project_name = ?", strings.TrimSpace(projectName)).Delete(&MemoryCleanupReview{})
	return result.RowsAffected, result.Error
}

// DeleteCleanupReviewsByProjectNameAndStatus 删除指定项目下的指定状态审核记录，避免项目合并误删已审核历史。
func (s *Store) DeleteCleanupReviewsByProjectNameAndStatus(projectName, status string) (int64, error) {
	result := s.db.Where("project_name = ? AND status = ?", strings.TrimSpace(projectName), strings.TrimSpace(status)).Delete(&MemoryCleanupReview{})
	return result.RowsAffected, result.Error
}

// UpdateCleanupReviewsStatusByProjectNameAndStatus 批量改写指定项目下的审核状态，避免项目合并后旧批准记录继续可执行。
func (s *Store) UpdateCleanupReviewsStatusByProjectNameAndStatus(projectName, fromStatus, toStatus, reviewedBy, reviewedAt, note string) (int64, error) {
	result := s.db.Model(&MemoryCleanupReview{}).
		Where("project_name = ? AND status = ?", strings.TrimSpace(projectName), strings.TrimSpace(fromStatus)).
		Updates(map[string]any{
			"status":         strings.TrimSpace(toStatus),
			"reviewed_by":    strings.TrimSpace(reviewedBy),
			"reviewed_at":    strings.TrimSpace(reviewedAt),
			"execution_note": strings.TrimSpace(note),
		})
	return result.RowsAffected, result.Error
}

// LockMemoryCleanupReviewsForUpdate 在事务里锁定审核记录，避免并发执行清理任务时重复删除同一批记忆。
func (s *Store) LockMemoryCleanupReviewsForUpdate(ids []int64) ([]MemoryCleanupReview, error) {
	if len(ids) == 0 {
		return []MemoryCleanupReview{}, nil
	}
	var items []MemoryCleanupReview
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Find(&items).Error
	return items, err
}
