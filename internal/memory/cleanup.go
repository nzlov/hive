package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

type cleanupCandidate struct {
	Memory          models.Memory
	Score           float64
	Detail          CleanupScoreDetail
	ProtectedTagHit bool
}

// EnsureProtectedTagsSeeded 把配置中的默认保护标签补种到数据库，避免首次启动后后台列表为空。
func (s *Service) EnsureProtectedTagsSeeded(ctx context.Context) error {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return err
	}
	schedule := s.cleanupScheduleConfig()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	items := make([]models.MemoryProtectedTag, 0, len(schedule.ProtectedTags))
	for _, tag := range schedule.ProtectedTags {
		cleaned := strings.TrimSpace(tag)
		if cleaned == "" {
			continue
		}
		items = append(items, models.MemoryProtectedTag{Tag: cleaned, Enabled: true, Source: "config", CreatedAt: now, UpdatedAt: now})
	}
	return store.UpsertProtectedTags(items)
}

// ListProtectedTags 返回全部保护标签，供后台治理页面展示和管理。
func (s *Service) ListProtectedTags(ctx context.Context) ([]models.MemoryProtectedTag, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return store.ListProtectedTags()
}

// CreateProtectedTag 创建保护标签，避免管理员只能通过手改配置维护白名单。
func (s *Service) CreateProtectedTag(ctx context.Context, tag, description string, enabled bool) (models.MemoryProtectedTag, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return models.MemoryProtectedTag{}, err
	}
	cleanedTag := strings.TrimSpace(tag)
	if cleanedTag == "" {
		return models.MemoryProtectedTag{}, fmt.Errorf("保护标签不能为空")
	}
	return store.CreateProtectedTag(models.MemoryProtectedTag{Tag: cleanedTag, Enabled: enabled, Description: strings.TrimSpace(description), Source: "manual"})
}

// UpdateProtectedTag 更新保护标签的可维护字段，确保管理员调整后立即影响后续清理候选计算。
func (s *Service) UpdateProtectedTag(ctx context.Context, id int64, tag, description string, enabled bool) (models.MemoryProtectedTag, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return models.MemoryProtectedTag{}, err
	}
	cleanedTag := strings.TrimSpace(tag)
	if cleanedTag == "" {
		return models.MemoryProtectedTag{}, fmt.Errorf("保护标签不能为空")
	}
	return store.UpdateProtectedTag(id, models.MemoryProtectedTag{Tag: cleanedTag, Enabled: enabled, Description: strings.TrimSpace(description), Source: "manual"})
}

// DeleteProtectedTag 删除保护标签，允许管理员撤销不再适用的长期保留规则。
func (s *Service) DeleteProtectedTag(ctx context.Context, id int64) error {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return err
	}
	return store.DeleteProtectedTag(id)
}

// ListCleanupReviews 返回清理审核分页结果，避免 HTTP 层直接依赖存储实现细节。
func (s *Service) ListCleanupReviews(ctx context.Context, status, memType, projectName string, page, pageSize int) ([]models.MemoryCleanupReview, int64, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	return store.ListCleanupReviews(status, memType, projectName, page, pageSize)
}

// ApproveCleanupReviews 批量批准待删候选，避免管理员逐条操作增加审核成本。
func (s *Service) ApproveCleanupReviews(ctx context.Context, ids []int64, reviewer string) error {
	return s.updateCleanupReviewsStatus(ctx, ids, "approved", reviewer)
}

// RejectCleanupReviews 批量拒绝待删候选，避免不应删除的记忆再次进入自动执行流程。
func (s *Service) RejectCleanupReviews(ctx context.Context, ids []int64, reviewer string) error {
	return s.updateCleanupReviewsStatus(ctx, ids, "rejected", reviewer)
}

func (s *Service) updateCleanupReviewsStatus(ctx context.Context, ids []int64, status, reviewer string) error {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return err
	}
	return store.UpdateCleanupReviewStatus(uniqueInt64(ids), status, strings.TrimSpace(reviewer), time.Now().UTC().Format(time.RFC3339Nano))
}

// ExecuteApprovedCleanupReviews 执行已批准的候选删除，并同步清理向量与缓存。
func (s *Service) ExecuteApprovedCleanupReviews(ctx context.Context, ids []int64, limit int, operator string) (CleanupExecuteResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return CleanupExecuteResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	selectedIDs := uniqueInt64(ids)
	var executedReviewCount int
	var executedMemoryCount int
	err = store.WithTx(func(txStore *models.Store) error {
		var reviews []models.MemoryCleanupReview
		if len(selectedIDs) > 0 {
			locked, err := txStore.LockMemoryCleanupReviewsForUpdate(selectedIDs)
			if err != nil {
				return err
			}
			for _, review := range locked {
				if review.Status == "approved" {
					reviews = append(reviews, review)
				}
			}
		} else {
			items, err := txStore.ListApprovedCleanupReviews(limit)
			if err != nil {
				return err
			}
			reviews = items
		}
		memoryIDs := make([]int64, 0, len(reviews))
		reviewIDs := make([]int64, 0, len(reviews))
		for _, review := range reviews {
			memoryIDs = append(memoryIDs, review.MemoryID)
			reviewIDs = append(reviewIDs, review.ID)
		}
		uniqueMemoryIDs := uniqueInt64(memoryIDs)
		if len(reviewIDs) == 0 {
			executedReviewCount = 0
			executedMemoryCount = 0
			return nil
		}
		if err := txStore.DeleteMemoryEmbeddingsByMemoryIDs(uniqueMemoryIDs); err != nil {
			return err
		}
		if err := txStore.DeleteMemoriesByIDs(uniqueMemoryIDs); err != nil {
			return err
		}
		if err := txStore.MarkCleanupReviewsExecuted(reviewIDs, strings.TrimSpace(operator), now, "执行成功"); err != nil {
			return err
		}
		executedReviewCount = len(reviewIDs)
		executedMemoryCount = len(uniqueMemoryIDs)
		return nil
	})
	if err != nil {
		return CleanupExecuteResult{}, err
	}
	if executedMemoryCount > 0 {
		s.invalidateSearchCache()
	}
	return CleanupExecuteResult{ExecutedReviewCount: executedReviewCount, ExecutedMemoryCount: executedMemoryCount, Message: fmt.Sprintf("已执行 %d 条清理记录，删除 %d 条记忆。", executedReviewCount, executedMemoryCount)}, nil
}

// RunMemoryCleanupOnce 生成一轮清理候选，并按模式选择写入待审核队列或自动执行。
func (s *Service) RunMemoryCleanupOnce(ctx context.Context) (CleanupRunResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return CleanupRunResult{}, err
	}
	runAt := time.Now().UTC()
	summaryReviews, err := s.buildCleanupReviews(ctx, "summary", runAt)
	if err != nil {
		return CleanupRunResult{}, err
	}
	errorReviews, err := s.buildCleanupReviews(ctx, "error", runAt)
	if err != nil {
		return CleanupRunResult{}, err
	}
	runAtString := runAt.Format(time.RFC3339Nano)
	if !s.cleanupDryRun() {
		if err := store.ReplacePendingCleanupReviews(runAtString, "summary", summaryReviews); err != nil {
			return CleanupRunResult{}, err
		}
		if err := store.ReplacePendingCleanupReviews(runAtString, "error", errorReviews); err != nil {
			return CleanupRunResult{}, err
		}
	}
	result := CleanupRunResult{
		RunAt:                 runAtString,
		Mode:                  s.cleanupMode(),
		SummaryCandidateCount: len(summaryReviews),
		ErrorCandidateCount:   len(errorReviews),
		Message:               fmt.Sprintf("已生成待清理候选：summary=%d，error=%d。", len(summaryReviews), len(errorReviews)),
	}
	if s.cleanupMode() == "auto" && !s.cleanupDryRun() {
		execResult, err := s.executeReviewsDirect(ctx, append(summaryReviews, errorReviews...), "system")
		if err != nil {
			return result, err
		}
		result.Message = execResult.Message
	}
	log.Printf("记忆清理任务完成: mode=%s summary=%d error=%d dry_run=%v", result.Mode, result.SummaryCandidateCount, result.ErrorCandidateCount, s.cleanupDryRun())
	return result, nil
}

func (s *Service) buildCleanupReviews(ctx context.Context, memType string, now time.Time) ([]models.MemoryCleanupReview, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return nil, err
	}
	policy := s.cleanupPolicy(memType)
	cutoff := now.AddDate(0, 0, -policy.BeforeDays).Format("20060102150405")
	scanLimit := maxInt(s.cleanupReviewTopN()*5, policy.BatchSize*5)
	candidates, err := store.ListMemoryCleanupCandidates(memType, cutoff, scanLimit)
	if err != nil {
		return nil, err
	}
	protectedTags, err := s.enabledProtectedTagSet(ctx)
	if err != nil {
		return nil, err
	}
	projectCounts, err := s.loadProjectCounts(store, memType, candidates)
	if err != nil {
		return nil, err
	}
	totalCount, err := store.CountMemoriesByType(memType)
	if err != nil {
		return nil, err
	}
	ready := make([]cleanupCandidate, 0, len(candidates))
	for _, item := range candidates {
		tags := models.DecodeTags(item.Tags)
		if hasProtectedTag(tags, protectedTags) {
			continue
		}
		detail := buildCleanupScoreDetail(item, policy, projectCounts[item.ProjectName], now)
		if detail.Total < policy.ScoreThreshold {
			continue
		}
		ready = append(ready, cleanupCandidate{Memory: item, Score: detail.Total, Detail: detail})
	}
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Score == ready[j].Score {
			return ready[i].Memory.Timestamp < ready[j].Memory.Timestamp
		}
		return ready[i].Score > ready[j].Score
	})
	selected := make([]cleanupCandidate, 0, minInt(policy.BatchSize, len(ready)))
	remainingType := totalCount
	remainingProject := cloneInt64Map(projectCounts)
	for _, candidate := range ready {
		if len(selected) >= policy.BatchSize || len(selected) >= s.cleanupReviewTopN() {
			break
		}
		if remainingType-1 < int64(policy.MinRemaining) {
			continue
		}
		if remainingProject[candidate.Memory.ProjectName]-1 < int64(policy.MinRemainingPerProject) {
			continue
		}
		selected = append(selected, candidate)
		remainingType--
		remainingProject[candidate.Memory.ProjectName]--
	}
	runAt := now.Format(time.RFC3339Nano)
	reviews := make([]models.MemoryCleanupReview, 0, len(selected))
	for _, candidate := range selected {
		reviews = append(reviews, models.MemoryCleanupReview{
			MemoryID:     candidate.Memory.ID,
			ProjectName:  candidate.Memory.ProjectName,
			Type:         candidate.Memory.Type,
			Status:       "pending",
			Score:        candidate.Score,
			ReasonJSON:   candidate.Detail.ToJSON(),
			SnapshotJSON: buildCleanupSnapshotJSON(candidate.Memory),
			RunAt:        runAt,
			CreatedAt:    runAt,
		})
	}
	return reviews, nil
}

func (s *Service) executeReviewsDirect(ctx context.Context, reviews []models.MemoryCleanupReview, operator string) (CleanupExecuteResult, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return CleanupExecuteResult{}, err
	}
	memoryIDs := make([]int64, 0, len(reviews))
	for _, review := range reviews {
		memoryIDs = append(memoryIDs, review.MemoryID)
	}
	uniqueMemoryIDs := uniqueInt64(memoryIDs)
	err = store.WithTx(func(txStore *models.Store) error {
		if err := txStore.DeleteMemoryEmbeddingsByMemoryIDs(uniqueMemoryIDs); err != nil {
			return err
		}
		return txStore.DeleteMemoriesByIDs(uniqueMemoryIDs)
	})
	if err != nil {
		return CleanupExecuteResult{}, err
	}
	if len(uniqueMemoryIDs) > 0 {
		s.invalidateSearchCache()
	}
	return CleanupExecuteResult{ExecutedReviewCount: len(reviews), ExecutedMemoryCount: len(uniqueMemoryIDs), Message: fmt.Sprintf("自动清理已删除 %d 条记忆。", len(uniqueMemoryIDs))}, nil
}

func buildCleanupSnapshotJSON(item models.Memory) string {
	payload, _ := json.Marshal(map[string]any{
		"id":           item.ID,
		"project_name": item.ProjectName,
		"git_branch":   item.GitBranch,
		"type":         item.Type,
		"title":        item.Title,
		"tags":         models.DecodeTags(item.Tags),
		"summary":      item.Summary,
		"timestamp":    item.Timestamp,
		"created_at":   item.CreatedAt,
		"use_count":    item.UseCount,
		"last_used_at": item.LastUsedAt,
	})
	return string(payload)
}

func buildCleanupScoreDetail(item models.Memory, policy config.MemoryCleanupPolicyConfig, projectCount int64, now time.Time) CleanupScoreDetail {
	ageDays := maxInt(0, int(now.Sub(parseTimestamp(item.Timestamp)).Hours()/24))
	lastUsedDays := maxInt(0, int(now.Sub(parseCleanupTime(item.LastUsedAt)).Hours()/24))
	if strings.TrimSpace(item.LastUsedAt) == "" {
		lastUsedDays = policy.BeforeDays * 4
	}
	ageScore := math.Min(float64(ageDays)/float64(maxInt(policy.BeforeDays*4, 1)), 1)
	lastUsedScore := math.Min(float64(lastUsedDays)/float64(maxInt(policy.BeforeDays*2, 1)), 1)
	projectPressureScore := 0.0
	if projectCount > int64(policy.MinRemainingPerProject) && projectCount > 0 {
		projectPressureScore = float64(projectCount-int64(policy.MinRemainingPerProject)) / float64(projectCount)
	}
	useCountScore := cleanupUseCountScore(item.UseCount)
	total := ageScore*policy.Weights.Age + useCountScore*policy.Weights.UseCount + lastUsedScore*policy.Weights.LastUsed + projectPressureScore*policy.Weights.ProjectPressure
	return CleanupScoreDetail{
		AgeDays:          ageDays,
		UseCount:         item.UseCount,
		LastUsedDays:     lastUsedDays,
		ProjectName:      item.ProjectName,
		ProjectTypeCount: projectCount,
		Total:            total,
		SubScores: map[string]float64{
			"age":              ageScore,
			"use_count":        useCountScore,
			"last_used":        lastUsedScore,
			"project_pressure": projectPressureScore,
		},
	}
}

func cleanupUseCountScore(useCount int64) float64 {
	switch {
	case useCount <= 0:
		return 1
	case useCount == 1:
		return 0.85
	case useCount == 2:
		return 0.7
	case useCount <= 5:
		return 0.45
	default:
		return 0.15
	}
}

func parseCleanupTime(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Now().UTC()
	}
	if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return parsed
	}
	return parseTimestamp(trimmed)
}

func (s *Service) loadProjectCounts(store *models.Store, memType string, candidates []models.Memory) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, item := range candidates {
		if _, ok := counts[item.ProjectName]; ok {
			continue
		}
		total, err := store.CountMemoriesByProjectAndType(item.ProjectName, memType)
		if err != nil {
			return nil, err
		}
		counts[item.ProjectName] = total
	}
	return counts, nil
}

func (s *Service) enabledProtectedTagSet(ctx context.Context) (map[string]struct{}, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return nil, err
	}
	items, err := store.ListEnabledProtectedTags()
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[strings.TrimSpace(item.Tag)] = struct{}{}
	}
	return result, nil
}

func hasProtectedTag(tags []string, protected map[string]struct{}) bool {
	for _, tag := range tags {
		if _, ok := protected[strings.TrimSpace(tag)]; ok {
			return true
		}
	}
	return false
}

func (s *Service) cleanupScheduleConfig() config.MemoryCleanupScheduleConfig {
	if s.config.ScheduleConfig == nil {
		return config.MemoryCleanupScheduleConfig{Spec: "0 3 * * *", Mode: "review", ReviewTopN: 200, ProtectedTags: []string{"核心故障", "架构决策"}}
	}
	return s.config.ScheduleConfig.MemoryCleanup
}

func (s *Service) cleanupMode() string {
	return s.cleanupScheduleConfig().Mode
}

func (s *Service) cleanupDryRun() bool {
	return s.cleanupScheduleConfig().DryRun
}

func (s *Service) cleanupReviewTopN() int {
	if value := s.cleanupScheduleConfig().ReviewTopN; value > 0 {
		return value
	}
	return 200
}

func (s *Service) cleanupPolicy(memType string) config.MemoryCleanupPolicyConfig {
	schedule := s.cleanupScheduleConfig()
	if strings.TrimSpace(memType) == "error" {
		return schedule.Error
	}
	return schedule.Summary
}

// recordSearchUsage 只在命中最终返回结果时累计使用次数，避免低置信度裁剪前的候选被误记为真实使用。
func (s *Service) recordSearchUsage(ctx context.Context, result SearchResult) error {
	ids := make([]int64, 0, len(result.ErrorHits)+len(result.SummaryHits))
	for _, hit := range result.ErrorHits {
		ids = append(ids, hit.ID)
	}
	for _, hit := range result.SummaryHits {
		ids = append(ids, hit.ID)
	}
	ids = uniqueInt64(ids)
	if len(ids) == 0 {
		return nil
	}
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return err
	}
	return store.IncrementMemoryUseCounts(ids, time.Now().UTC().Format(time.RFC3339Nano))
}

func uniqueInt64(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
