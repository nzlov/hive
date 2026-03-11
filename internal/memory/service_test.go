package memory

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

// stubEmbeddingProvider 伪造稳定向量返回，避免测试依赖外部嵌入服务可用性。
type stubEmbeddingProvider struct {
	// enabled 控制 provider 是否开启，便于测试覆盖有无向量两种路径。
	enabled bool
	// model 保存测试模型名，便于断言元数据是否按预期更新。
	model string
	// vector 保存固定返回向量，避免测试受随机结果干扰。
	vector []float64
	// calls 统计调用次数，便于断言缓存与重建逻辑是否生效。
	calls int64
}

// queryEmbeddingProvider 只为查询阶段返回固定向量，便于测试候选筛选与分页逻辑。
type queryEmbeddingProvider struct {
	// vector 保存查询固定向量，便于构造稳定的语义命中结果。
	vector []float64
	// calls 统计嵌入调用次数，便于验证并发去重与缓存逻辑。
	calls int64
}

// textAwareEmbeddingProvider 根据输入文本生成不同向量，便于验证编辑后向量是否同步刷新。
type textAwareEmbeddingProvider struct {
	// model 保存测试模型名，便于断言更新后元数据行为稳定。
	model string
	// calls 统计调用次数，便于验证编辑前后是否都触发重算。
	calls int
}

// Enabled 让语义搜索路径保持开启，避免测试退化为关键字分支。
func (p *queryEmbeddingProvider) Enabled() bool { return true }

// ModelName 返回固定模型名，避免测试被模型元数据分支干扰。
func (p *queryEmbeddingProvider) ModelName() string { return "query-only-model" }

// EmbedTexts 为每个检索输入返回同一查询向量，让测试聚焦候选过滤而非远程协议。
func (p *queryEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) {
	atomic.AddInt64(&p.calls, 1)
	vectors := make([][]float64, 0, len(texts))
	for range texts {
		vector := make([]float64, len(p.vector))
		copy(vector, p.vector)
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

// Enabled 让编辑测试始终走向量更新路径，避免退化为仅更新主记录。
func (p *textAwareEmbeddingProvider) Enabled() bool { return true }

// ModelName 返回固定模型名，保证测试里的元数据行为稳定可预测。
func (p *textAwareEmbeddingProvider) ModelName() string { return p.model }

// EmbedTexts 根据文本长度和首字符生成可区分向量，方便断言编辑前后向量已变化。
func (p *textAwareEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) {
	p.calls++
	vectors := make([][]float64, 0, len(texts))
	for _, text := range texts {
		runes := []rune(strings.TrimSpace(text))
		first := 0.0
		if len(runes) > 0 {
			first = float64(runes[0])
		}
		vectors = append(vectors, []float64{float64(len(runes)), first})
	}
	return vectors, nil
}

// Enabled 让测试显式控制当前 provider 是否启用，避免不同分支隐式耦合。
func (p *stubEmbeddingProvider) Enabled() bool { return p.enabled }

// ModelName 返回固定模型名，方便验证模型切换后的元数据是否同步更新。
func (p *stubEmbeddingProvider) ModelName() string { return p.model }

// EmbedTexts 为每条输入返回同一组向量，让测试只关注重建触发条件而非算法细节。
func (p *stubEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) {
	atomic.AddInt64(&p.calls, 1)
	vectors := make([][]float64, 0, len(texts))
	for range texts {
		vector := make([]float64, len(p.vector))
		copy(vector, p.vector)
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

// testContextWithStore 为测试注入共享 Store，避免服务层继续显式打开数据库连接。
func testContextWithStore(t *testing.T, cfg config.AppConfig) context.Context {
	t.Helper()
	store, err := models.Open(cfg)
	if err != nil {
		t.Fatalf("打开模型存储失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("关闭模型存储失败: %v", closeErr)
		}
	})
	return models.StoreToContext(context.Background(), store)
}

// seedSemanticMemory 直接写入记忆和向量，避免测试依赖写入阶段的嵌入生成策略。
func seedSemanticMemory(t *testing.T, store *models.Store, projectName, memType, title, content, timestamp string, vector []float64) int64 {
	t.Helper()
	items, err := store.CreateMemories([]models.Memory{{
		UserID:      "semantic-test-user",
		ProjectName: projectName,
		GitBranch:   "feature/semantic-test",
		Type:        memType,
		Title:       title,
		Tags:        models.EncodeTags([]string{"语义", "测试"}),
		Summary:     "用于验证语义候选筛选。",
		Content:     content,
		Timestamp:   timestamp,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}})
	if err != nil {
		t.Fatalf("写入语义测试记忆失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("语义测试记忆写入数量异常: %d", len(items))
	}
	if err := store.UpsertMemoryEmbeddings([]models.MemoryEmbedding{{
		MemoryID:    items[0].ID,
		ProjectName: projectName,
		Type:        memType,
		Vector:      models.EncodeVector(vector),
		Timestamp:   timestamp,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}}); err != nil {
		t.Fatalf("写入语义测试向量失败: %v", err)
	}
	return items[0].ID
}

// TestServiceWriteAndSearch 验证服务层可以完成写入和检索，避免 HTTP 之下的核心流程回归失效。
func TestServiceWriteAndSearch(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{
		MemoryRoot:       t.TempDir(),
		ServerBaseURL:    "http://127.0.0.1:18080",
		ServerListenAddr: ":18080",
	})
	ctx := testContextWithStore(t, service.config)

	databasePath, err := service.Write(ctx, "service-alias", "feature/test-branch", "test-userid", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "服务层写入测试",
		Tags:    []string{"服务层", "测试"},
		Summary: "验证服务层写入后能够被检索命中。",
		Context: "## Summary\n\n- 详情: 服务层测试写入内容。",
	}})
	if err != nil {
		t.Fatalf("Write 返回错误: %v", err)
	}
	if !strings.HasSuffix(databasePath, "memory.db") {
		t.Fatalf("Write 返回的数据库路径不符合预期: %s", databasePath)
	}

	result, err := service.Search(ctx, "service-alias", nil, "服务层写入测试", true)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	markdown := result.Markdown()
	if !strings.Contains(markdown, "Summary Hits (1)") {
		t.Fatalf("Search 结果未包含总结命中: %s", markdown)
	}
	if !strings.Contains(markdown, "服务层写入测试") {
		t.Fatalf("Search 结果未包含写入标题: %s", markdown)
	}
	if !strings.Contains(markdown, "Debug Commands") {
		t.Fatalf("Search 调试输出缺少 Debug Commands: %s", markdown)
	}
	if !strings.Contains(markdown, "project_name: service-alias") {
		t.Fatalf("Search 调试输出缺少项目名: %s", markdown)
	}
	if len(result.SummaryHits) != 1 {
		t.Fatalf("Search 结果数量异常: %+v", result.SummaryHits)
	}
	if result.SummaryHits[0].GitBranch != "feature/test-branch" {
		t.Fatalf("Search 结果未返回 git 分支: %+v", result.SummaryHits[0])
	}
	if result.SummaryHits[0].Title != "服务层写入测试" {
		t.Fatalf("Search 结果未返回标题: %+v", result.SummaryHits[0])
	}
	if strings.Join(result.SummaryHits[0].Tags, ",") != "服务层,测试" {
		t.Fatalf("Search 结果未返回标签: %+v", result.SummaryHits[0])
	}
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	items, err := store.ListAllMemories()
	if err != nil {
		t.Fatalf("读取记忆失败: %v", err)
	}
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		rows = append(rows, memoryRowFromModel(item))
	}
	if len(rows) != 1 || rows[0].UserID != "test-userid" {
		t.Fatalf("写入记忆未落库 userid: %+v", rows)
	}
	if rows[0].GitBranch != "feature/test-branch" {
		t.Fatalf("写入记忆未落库 git_branch: %+v", rows)
	}
}

// TestServiceUpdateRefreshesEmbedding 验证编辑记忆后会同步刷新向量内容，避免语义检索继续命中旧文本。
func TestServiceUpdateRefreshesEmbedding(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	provider := &textAwareEmbeddingProvider{model: "update-aware-model"}
	service.provider = provider
	ctx := testContextWithStore(t, service.config)

	if _, err := service.Write(ctx, "update-project", "feature/edit", "editor-user", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "编辑前标题",
		Tags:    []string{"编辑前", "标签"},
		Summary: "编辑前总结",
		Context: "编辑前正文",
	}}); err != nil {
		t.Fatalf("写入测试记忆失败: %v", err)
	}
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	items, err := store.ListAllMemories()
	if err != nil || len(items) != 1 {
		t.Fatalf("读取初始记忆失败: err=%v items=%+v", err, items)
	}
	memoryID := items[0].ID
	beforeEmbedding, err := store.ListMemoryEmbeddings("update-project", []int64{memoryID})
	if err != nil {
		t.Fatalf("读取初始向量失败: %v", err)
	}
	beforeVector := beforeEmbedding[memoryID]
	if len(beforeVector) == 0 {
		t.Fatalf("初始向量为空: %+v", beforeEmbedding)
	}

	updated, err := service.Update(ctx, memoryID, api.UpdateMemoryRequest{
		Title:   "编辑后标题",
		Tags:    []string{"编辑后", "向量同步"},
		Summary: "编辑后总结",
		Content: "编辑后正文\n\n包含更多内容用于触发不同向量。",
	})
	if err != nil {
		t.Fatalf("更新记忆失败: %v", err)
	}
	if updated.Title != "编辑后标题" || updated.Summary != "编辑后总结" {
		t.Fatalf("更新后的主记录字段异常: %+v", updated)
	}
	if updated.ProjectName != "update-project" || updated.Type != "summary" {
		t.Fatalf("更新不应修改不可编辑字段: %+v", updated)
	}
	afterEmbedding, err := store.ListMemoryEmbeddings("update-project", []int64{memoryID})
	if err != nil {
		t.Fatalf("读取更新后向量失败: %v", err)
	}
	afterVector := afterEmbedding[memoryID]
	if len(afterVector) == 0 {
		t.Fatalf("更新后向量为空: %+v", afterEmbedding)
	}
	if len(beforeVector) != len(afterVector) {
		t.Fatalf("更新前后向量维度不一致: before=%v after=%v", beforeVector, afterVector)
	}
	if beforeVector[0] == afterVector[0] && beforeVector[1] == afterVector[1] {
		t.Fatalf("编辑后向量未刷新: before=%v after=%v", beforeVector, afterVector)
	}
	if provider.calls < 2 {
		t.Fatalf("编辑前后都应触发嵌入生成: calls=%d", provider.calls)
	}
	stored, err := store.GetMemoryByID(memoryID)
	if err != nil {
		t.Fatalf("回读更新后的记忆失败: %v", err)
	}
	if stored.GitBranch != "feature/edit" || stored.CreatedAt == "" || stored.Timestamp == "" {
		t.Fatalf("更新后应保留原始元数据: %+v", stored)
	}
}

// TestServiceMergeProjectMemoriesBatchesIDs 验证项目合并会按 ID 分页迁移并重建向量，避免大项目一次性占用过多内存。
func TestServiceMergeProjectMemoriesBatchesIDs(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	provider := &stubEmbeddingProvider{enabled: true, model: "merge-batch-model", vector: []float64{0.6, 0.4}}
	service.provider = provider
	ctx := testContextWithStore(t, service.config)
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	if _, err := service.Write(ctx, "main-project", "main", "admin", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "主项目记忆",
		Tags:    []string{"主项目"},
		Summary: "主项目保留",
		Context: "主项目保留正文",
	}}); err != nil {
		t.Fatalf("写入主项目记忆失败: %v", err)
	}
	items := make([]api.MemoryWriteItem, 0, 205)
	for idx := 0; idx < 205; idx++ {
		items = append(items, api.MemoryWriteItem{
			Type:    "summary",
			Title:   fmt.Sprintf("副项目记忆-%03d", idx),
			Tags:    []string{"副项目", "分页"},
			Summary: fmt.Sprintf("副项目摘要-%03d", idx),
			Context: fmt.Sprintf("副项目正文-%03d", idx),
		})
	}
	if _, err := service.Write(ctx, "alias-project", "feature/merge", "admin", items); err != nil {
		t.Fatalf("写入副项目记忆失败: %v", err)
	}
	if err := store.ReplacePendingCleanupReviews(time.Now().UTC().Format(time.RFC3339Nano), "summary", []models.MemoryCleanupReview{{
		MemoryID:     1,
		ProjectName:  "alias-project",
		Type:         "summary",
		Status:       "pending",
		Score:        0.9,
		ReasonJSON:   "{}",
		SnapshotJSON: "{}",
		RunAt:        time.Now().UTC().Format(time.RFC3339Nano),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}}); err != nil {
		t.Fatalf("写入副项目审核记录失败: %v", err)
	}
	approvedAliasReview := models.MemoryCleanupReview{
		MemoryID:     1,
		ProjectName:  "alias-project",
		Type:         "summary",
		Status:       "approved",
		Score:        0.6,
		ReasonJSON:   "{}",
		SnapshotJSON: "{}",
		RunAt:        time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano),
		CreatedAt:    time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano),
	}
	if err := store.ReplacePendingCleanupReviews(approvedAliasReview.RunAt, "summary", []models.MemoryCleanupReview{approvedAliasReview}); err != nil {
		t.Fatalf("写入副项目已批准审核记录失败: %v", err)
	}
	if err := store.ReplacePendingCleanupReviews(time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano), "summary", []models.MemoryCleanupReview{{
		MemoryID:     2,
		ProjectName:  "main-project",
		Type:         "summary",
		Status:       "pending",
		Score:        0.7,
		ReasonJSON:   "{}",
		SnapshotJSON: "{}",
		RunAt:        time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano),
		CreatedAt:    time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano),
	}}); err != nil {
		t.Fatalf("写入主项目审核记录失败: %v", err)
	}

	result, err := service.MergeProjectMemories(ctx, "alias-project", "main-project")
	if err != nil {
		t.Fatalf("合并项目记忆失败: %v", err)
	}
	if result.BatchCount != 2 {
		t.Fatalf("分页批次数异常: %+v", result)
	}
	if result.MergedMemoryCount != 205 || result.RebuiltEmbeddingCount != 205 {
		t.Fatalf("合并结果计数异常: %+v", result)
	}
	if result.ClearedReviewCount != 1 {
		t.Fatalf("清理审核记录数量异常: %+v", result)
	}
	if result.InvalidatedApprovedReviewCount != 1 {
		t.Fatalf("已批准记录失效数量异常: %+v", result)
	}
	aliasItems, err := store.ListMemoriesByProjectAndType("alias-project", "summary")
	if err != nil {
		t.Fatalf("读取副项目记忆失败: %v", err)
	}
	if len(aliasItems) != 0 {
		t.Fatalf("副项目记忆应已全部迁移: %d", len(aliasItems))
	}
	mainItems, err := store.ListMemoriesByProjectAndType("main-project", "summary")
	if err != nil {
		t.Fatalf("读取主项目记忆失败: %v", err)
	}
	if len(mainItems) != 206 {
		t.Fatalf("主项目记忆数量异常: %d", len(mainItems))
	}
	mergedIDs := make([]int64, 0, 205)
	for _, item := range mainItems {
		if strings.HasPrefix(item.Title, "副项目记忆-") {
			mergedIDs = append(mergedIDs, item.ID)
		}
	}
	embeddings, err := store.ListMemoryEmbeddings("main-project", mergedIDs)
	if err != nil {
		t.Fatalf("读取合并后向量失败: %v", err)
	}
	if len(embeddings) != 205 {
		t.Fatalf("合并后向量数量异常: %d", len(embeddings))
	}
	reviews, total, err := store.ListCleanupReviews("pending", "summary", "alias-project", 1, 10)
	if err != nil {
		t.Fatalf("读取副项目审核记录失败: %v", err)
	}
	if total != 0 || len(reviews) != 0 {
		t.Fatalf("副项目待审核记录应已清空: total=%d items=%+v", total, reviews)
	}
	approvedReviews, approvedTotal, err := store.ListCleanupReviews("approved", "summary", "alias-project", 1, 10)
	if err != nil {
		t.Fatalf("读取副项目已批准审核记录失败: %v", err)
	}
	if approvedTotal != 0 || len(approvedReviews) != 0 {
		t.Fatalf("副项目已批准记录应已失效: total=%d items=%+v", approvedTotal, approvedReviews)
	}
	rejectedReviews, rejectedTotal, err := store.ListCleanupReviews("rejected", "summary", "alias-project", 1, 10)
	if err != nil {
		t.Fatalf("读取副项目已失效审核记录失败: %v", err)
	}
	if rejectedTotal != 1 || len(rejectedReviews) != 1 {
		t.Fatalf("副项目已批准记录应改写为已拒绝: total=%d items=%+v", rejectedTotal, rejectedReviews)
	}
	if !strings.Contains(rejectedReviews[0].ExecutionNote, "已合并") {
		t.Fatalf("失效记录缺少项目合并说明: %+v", rejectedReviews[0])
	}
	execResult, err := service.ExecuteApprovedCleanupReviews(ctx, nil, 20, "tester")
	if err != nil {
		t.Fatalf("执行已批准清理失败: %v", err)
	}
	if execResult.ExecutedReviewCount != 0 || execResult.ExecutedMemoryCount != 0 {
		t.Fatalf("副项目旧批准记录失效后不应再删除记忆: %+v", execResult)
	}
	mainReviews, mainTotal, err := store.ListCleanupReviews("", "summary", "main-project", 1, 10)
	if err != nil {
		t.Fatalf("读取主项目审核记录失败: %v", err)
	}
	if mainTotal != 1 || len(mainReviews) != 1 {
		t.Fatalf("主项目审核记录不应受影响: total=%d items=%+v", mainTotal, mainReviews)
	}
}

// TestServiceMergeMemoryTagsKeepsProjectScope 验证标签合并只影响指定项目，并同步去重标签、重建向量和清理相关审核记录。
func TestServiceMergeMemoryTagsKeepsProjectScope(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	provider := &textAwareEmbeddingProvider{model: "tag-merge-model"}
	service.provider = provider
	ctx := testContextWithStore(t, service.config)
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	if _, err := service.Write(ctx, "target-project", "main", "admin", []api.MemoryWriteItem{
		{Type: "summary", Title: "标签待合并-1", Tags: []string{"旧标签", "重复标签", "主标签"}, Summary: "待合并一", Context: "正文一"},
		{Type: "summary", Title: "标签待合并-2", Tags: []string{"旧标签", "待统一"}, Summary: "待合并二", Context: "正文二"},
		{Type: "summary", Title: "仅主标签", Tags: []string{"主标签"}, Summary: "保持不变", Context: "正文三"},
	}); err != nil {
		t.Fatalf("写入目标项目记忆失败: %v", err)
	}
	if _, err := service.Write(ctx, "other-project", "main", "admin", []api.MemoryWriteItem{{Type: "summary", Title: "其他项目", Tags: []string{"旧标签"}, Summary: "其他", Context: "其他正文"}}); err != nil {
		t.Fatalf("写入其他项目记忆失败: %v", err)
	}
	targetItems, err := store.ListMemoriesByProjectAndType("target-project", "summary")
	if err != nil {
		t.Fatalf("读取目标项目记忆失败: %v", err)
	}
	affectedIDs := make([]int64, 0, 2)
	for _, item := range targetItems {
		if strings.HasPrefix(item.Title, "标签待合并-") {
			affectedIDs = append(affectedIDs, item.ID)
		}
	}
	beforeEmbeddings, err := store.ListMemoryEmbeddings("target-project", affectedIDs)
	if err != nil {
		t.Fatalf("读取合并前向量失败: %v", err)
	}
	runAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(runAt, "summary", []models.MemoryCleanupReview{
		{MemoryID: affectedIDs[0], ProjectName: "target-project", Type: "summary", Status: "pending", Score: 0.9, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: runAt, CreatedAt: runAt},
		{MemoryID: affectedIDs[1], ProjectName: "target-project", Type: "summary", Status: "pending", Score: 0.8, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: runAt, CreatedAt: runAt},
	}); err != nil {
		t.Fatalf("写入待审核记录失败: %v", err)
	}
	approvedAt := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(approvedAt, "summary", []models.MemoryCleanupReview{{MemoryID: affectedIDs[0], ProjectName: "target-project", Type: "summary", Status: "approved", Score: 0.7, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: approvedAt, CreatedAt: approvedAt}}); err != nil {
		t.Fatalf("写入已批准记录失败: %v", err)
	}
	executedAt := time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(executedAt, "summary", []models.MemoryCleanupReview{{MemoryID: affectedIDs[0], ProjectName: "target-project", Type: "summary", Status: "executed", Score: 0.6, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: executedAt, CreatedAt: executedAt}}); err != nil {
		t.Fatalf("写入已执行记录失败: %v", err)
	}
	otherItems, err := store.ListMemoriesByProjectAndType("other-project", "summary")
	if err != nil || len(otherItems) != 1 {
		t.Fatalf("读取其他项目记忆失败: err=%v items=%+v", err, otherItems)
	}
	otherRunAt := time.Now().UTC().Add(3 * time.Second).Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(otherRunAt, "summary", []models.MemoryCleanupReview{{MemoryID: otherItems[0].ID, ProjectName: "other-project", Type: "summary", Status: "pending", Score: 0.5, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: otherRunAt, CreatedAt: otherRunAt}}); err != nil {
		t.Fatalf("写入其他项目审核记录失败: %v", err)
	}

	result, err := service.MergeMemoryTags(ctx, "target-project", []string{"旧标签", "待统一"}, "主标签")
	if err != nil {
		t.Fatalf("合并标签失败: %v", err)
	}
	if result.AffectedMemoryCount != 2 || result.RebuiltEmbeddingCount != 2 {
		t.Fatalf("标签合并结果计数异常: %+v", result)
	}
	if result.ClearedPendingReviewCount != 2 || result.InvalidatedApprovedReviewCount != 1 {
		t.Fatalf("标签合并审批清理统计异常: %+v", result)
	}
	updatedItems, err := store.ListMemoriesByProjectAndType("target-project", "summary")
	if err != nil {
		t.Fatalf("读取合并后目标项目记忆失败: %v", err)
	}
	for _, item := range updatedItems {
		tags := models.DecodeTags(item.Tags)
		if strings.HasPrefix(item.Title, "标签待合并-") {
			if hasAnyTag(tags, []string{"旧标签", "待统一"}) {
				t.Fatalf("副标签应已被移除: %+v", tags)
			}
			if countTag(tags, "主标签") != 1 {
				t.Fatalf("主标签应去重后只保留一份: %+v", tags)
			}
		}
	}
	afterEmbeddings, err := store.ListMemoryEmbeddings("target-project", affectedIDs)
	if err != nil {
		t.Fatalf("读取合并后向量失败: %v", err)
	}
	for _, id := range affectedIDs {
		beforeVector := beforeEmbeddings[id]
		afterVector := afterEmbeddings[id]
		if len(afterVector) == 0 {
			t.Fatalf("合并后向量为空: id=%d", id)
		}
		if len(beforeVector) == len(afterVector) && len(afterVector) > 0 {
			unchanged := true
			for idx := range afterVector {
				if beforeVector[idx] != afterVector[idx] {
					unchanged = false
					break
				}
			}
			if unchanged {
				t.Fatalf("标签合并后向量应已刷新: id=%d before=%v after=%v", id, beforeVector, afterVector)
			}
		}
	}
	otherUpdatedItems, err := store.ListMemoriesByProjectAndType("other-project", "summary")
	if err != nil {
		t.Fatalf("读取其他项目记忆失败: %v", err)
	}
	if countTag(models.DecodeTags(otherUpdatedItems[0].Tags), "旧标签") != 1 {
		t.Fatalf("其他项目标签不应被改动: %+v", models.DecodeTags(otherUpdatedItems[0].Tags))
	}
	pendingReviews, pendingTotal, err := store.ListCleanupReviews("pending", "summary", "target-project", 1, 10)
	if err != nil {
		t.Fatalf("读取目标项目待审核记录失败: %v", err)
	}
	if pendingTotal != 0 || len(pendingReviews) != 0 {
		t.Fatalf("目标项目待审核记录应已清空: total=%d items=%+v", pendingTotal, pendingReviews)
	}
	rejectedReviews, rejectedTotal, err := store.ListCleanupReviews("rejected", "summary", "target-project", 1, 10)
	if err != nil {
		t.Fatalf("读取目标项目已失效记录失败: %v", err)
	}
	if rejectedTotal != 1 || len(rejectedReviews) != 1 {
		t.Fatalf("目标项目已批准记录应被失效化: total=%d items=%+v", rejectedTotal, rejectedReviews)
	}
	if !strings.Contains(rejectedReviews[0].ExecutionNote, "标签合并") {
		t.Fatalf("已失效记录缺少标签合并说明: %+v", rejectedReviews[0])
	}
	executedReviews, executedTotal, err := store.ListCleanupReviews("executed", "summary", "target-project", 1, 10)
	if err != nil {
		t.Fatalf("读取目标项目已执行记录失败: %v", err)
	}
	if executedTotal != 1 || len(executedReviews) != 1 {
		t.Fatalf("已执行记录应保留不动: total=%d items=%+v", executedTotal, executedReviews)
	}
	otherPendingReviews, otherPendingTotal, err := store.ListCleanupReviews("pending", "summary", "other-project", 1, 10)
	if err != nil {
		t.Fatalf("读取其他项目待审核记录失败: %v", err)
	}
	if otherPendingTotal != 1 || len(otherPendingReviews) != 1 {
		t.Fatalf("其他项目待审核记录不应被清理: total=%d items=%+v", otherPendingTotal, otherPendingReviews)
	}
}

// countTag 统计标签出现次数，避免测试里遗漏重复标签清理问题。
func countTag(tags []string, target string) int {
	count := 0
	for _, tag := range tags {
		if tag == target {
			count++
		}
	}
	return count
}

// TestServiceSearchReturnsBranchMetadata 验证查询会返回分支元信息，后续由脚本决定是否保留该条记忆。
func TestServiceSearchReturnsBranchMetadata(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)

	if _, err := service.Write(ctx, "branch-project", "feature/a", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "A分支记忆",
		Tags:    []string{"分支"},
		Summary: "仅 feature/a 可见。",
		Context: "## Summary\n\n- 详情: A 分支的结论。",
	}}); err != nil {
		t.Fatalf("写入 feature/a 记忆失败: %v", err)
	}
	if _, err := service.Write(ctx, "branch-project", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "公共记忆",
		Tags:    []string{"公共"},
		Summary: "所有分支都可见。",
		Context: "## Summary\n\n- 详情: 公共记忆。",
	}}); err != nil {
		t.Fatalf("写入公共记忆失败: %v", err)
	}

	result, err := service.Search(ctx, "branch-project", nil, "记忆", false)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	if len(result.SummaryHits) != 2 {
		t.Fatalf("Search 分支过滤数量异常: %+v", result.SummaryHits)
	}
	branches := []string{result.SummaryHits[0].GitBranch, result.SummaryHits[1].GitBranch}
	if !strings.Contains(strings.Join(branches, ","), "feature/a") {
		t.Fatalf("Search 结果缺少分支记忆元信息: %+v", result.SummaryHits)
	}
	if !strings.Contains(result.Markdown(), "公共记忆") {
		t.Fatalf("Search 结果缺少公共记忆: %s", result.Markdown())
	}
	if !strings.Contains(result.Markdown(), "A分支记忆") {
		t.Fatalf("Search 结果应返回分支记忆供脚本筛选: %s", result.Markdown())
	}
}

// TestServiceSearchSnippetIncludesTitleAndTags 验证服务端返回片段时会补齐标题与标签，避免片段脱离上下文后难以理解。
func TestServiceSearchSnippetIncludesTitleAndTags(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})

	ctx := testContextWithStore(t, service.config)

	if _, err := service.Write(ctx, "snippet-project", "feature/snippet", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "连接池复用策略",
		Tags:    []string{"数据库", "连接池"},
		Summary: "记录连接池复用的调优经验。",
		Context: "## Summary\n\n- 现象: 查询高峰期连接数抖动。\n\n## Fix\n\n- 方案: 统一复用长连接池，避免频繁创建连接。",
	}}); err != nil {
		t.Fatalf("写入片段记忆失败: %v", err)
	}

	result, err := service.Search(ctx, "snippet-project", nil, "长连接池", false)
	if err != nil {
		t.Fatalf("搜索片段记忆失败: %v", err)
	}
	if len(result.SummaryHits) != 1 {
		t.Fatalf("片段搜索结果数量异常: %+v", result.SummaryHits)
	}
	if len(result.SummaryHits[0].Snippets) == 0 {
		t.Fatalf("片段搜索结果未返回 snippets: %+v", result.SummaryHits[0])
	}
	if result.SummaryHits[0].Title != "连接池复用策略" {
		t.Fatalf("片段搜索结果缺少标题字段: %+v", result.SummaryHits[0])
	}
	if strings.Join(result.SummaryHits[0].Tags, ",") != "数据库,连接池" {
		t.Fatalf("片段搜索结果缺少标签字段: %+v", result.SummaryHits[0])
	}
	content := result.SummaryHits[0].Snippets[0].Content
	if !strings.Contains(content, "长连接池") {
		t.Fatalf("片段缺少命中正文: %s", content)
	}
	if strings.Contains(result.Markdown(), "- path:") {
		t.Fatalf("Markdown 不应再渲染 path 字段: %s", result.Markdown())
	}
	if strings.Contains(result.Markdown(), "title: 连接池复用策略\n\ntitle:") {
		t.Fatalf("Markdown 不应重复渲染已移除的头部前缀: %s", result.Markdown())
	}
}

// TestServiceSearchIsolatedByProjectName 验证单库模式下不同项目仍会按项目名隔离搜索结果，避免跨项目串记忆。
func TestServiceSearchIsolatedByProjectName(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)
	if _, err := service.Write(ctx, "project-a", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "项目A记忆",
		Tags:    []string{"A"},
		Summary: "只属于项目A。",
		Context: "## Summary\n\n- 详情: 项目 A 的记忆。",
	}}); err != nil {
		t.Fatalf("写入项目A记忆失败: %v", err)
	}
	if _, err := service.Write(ctx, "project-b", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "项目B记忆",
		Tags:    []string{"B"},
		Summary: "只属于项目B。",
		Context: "## Summary\n\n- 详情: 项目 B 的记忆。",
	}}); err != nil {
		t.Fatalf("写入项目B记忆失败: %v", err)
	}

	resultA, err := service.Search(ctx, "project-a", nil, "记忆", false)
	if err != nil {
		t.Fatalf("搜索项目A记忆失败: %v", err)
	}
	if len(resultA.SummaryHits) != 1 {
		t.Fatalf("项目A搜索结果未正确隔离: %+v", resultA.SummaryHits)
	}
	if strings.Contains(resultA.Markdown(), "项目B记忆") {
		t.Fatalf("项目A搜索结果串入了项目B记忆: %s", resultA.Markdown())
	}

	resultB, err := service.Search(ctx, "project-b", nil, "记忆", false)
	if err != nil {
		t.Fatalf("搜索项目B记忆失败: %v", err)
	}
	if len(resultB.SummaryHits) != 1 {
		t.Fatalf("项目B搜索结果未正确隔离: %+v", resultB.SummaryHits)
	}
	if strings.Contains(resultB.Markdown(), "项目A记忆") {
		t.Fatalf("项目B搜索结果串入了项目A记忆: %s", resultB.Markdown())
	}
}

// TestServiceEnsureEmbeddingsReadyRebuildsOnModelMismatch 验证服务启动时会在模型不一致时自动重建向量。
func TestServiceEnsureEmbeddingsReadyRebuildsOnModelMismatch(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)
	oldProvider := &stubEmbeddingProvider{enabled: true, model: "old-model", vector: []float64{1, 0}}
	service.provider = oldProvider

	if _, err := service.Write(ctx, "rebuild-project", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "模型切换记忆",
		Tags:    []string{"重建"},
		Summary: "先写入旧模型向量，再验证启动时重建。",
		Context: "## Summary\n\n- 详情: 旧模型写入。",
	}}); err != nil {
		t.Fatalf("写入旧模型记忆失败: %v", err)
	}
	if got := atomic.LoadInt64(&oldProvider.calls); got != 1 {
		t.Fatalf("旧模型写入时应生成一次向量，实际次数=%d", got)
	}

	newProvider := &stubEmbeddingProvider{enabled: true, model: "new-model", vector: []float64{0, 1}}
	service.provider = newProvider
	result, err := service.EnsureEmbeddingsReady(ctx)
	if err != nil {
		t.Fatalf("启动校验嵌入模型失败: %v", err)
	}
	if !result.Changed {
		t.Fatalf("模型切换后应触发重建: %+v", result)
	}
	if !strings.Contains(result.Message, "new-model") {
		t.Fatalf("重建结果未包含新模型名: %+v", result)
	}
	if got := atomic.LoadInt64(&newProvider.calls); got != 1 {
		t.Fatalf("模型切换后应执行一次重建，实际次数=%d", got)
	}

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}

	currentModel, err := store.GetMemoryMetadata(embeddingModelMetaKey)
	if err != nil {
		t.Fatalf("读取模型元数据失败: %v", err)
	}
	if currentModel != "new-model" {
		t.Fatalf("模型元数据未更新: %s", currentModel)
	}
	items, err := store.ListAllMemories()
	if err != nil {
		t.Fatalf("读取记忆失败: %v", err)
	}
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		rows = append(rows, memoryRowFromModel(item))
	}
	if len(rows) != 1 {
		t.Fatalf("测试数据数量异常: %d", len(rows))
	}
	embeddings, err := store.ListMemoryEmbeddings("rebuild-project", []int64{rows[0].ID})
	if err != nil {
		t.Fatalf("读取向量失败: %v", err)
	}
	vector, ok := embeddings[rows[0].ID]
	if !ok {
		t.Fatalf("重建后缺少向量记录")
	}
	if len(vector) != 2 || math.Abs(vector[0]-0) > 1e-9 || math.Abs(vector[1]-1) > 1e-9 {
		t.Fatalf("向量未按新模型重建: %+v", vector)
	}
}

// TestServiceEnsureEmbeddingsReadyKeepsCurrentModelWhenCountMatches 验证模型未变化且数量一致时不会触发重建。
func TestServiceEnsureEmbeddingsReadyRebuildsLegacyEmbeddings(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)
	provider := &stubEmbeddingProvider{enabled: true, model: "legacy-compatible-model", vector: []float64{0.2, 0.8}}
	service.provider = provider

	if _, err := service.Write(ctx, "legacy-project", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "旧版向量记忆",
		Tags:    []string{"迁移"},
		Summary: "先写入一条完整向量，再模拟旧版字段缺失。",
		Context: "## Summary\n\n- 详情: 需要触发补全重建。",
	}}); err != nil {
		t.Fatalf("写入旧版测试记忆失败: %v", err)
	}
	if got := atomic.LoadInt64(&provider.calls); got != 1 {
		t.Fatalf("初次写入应生成一次向量，实际次数=%d", got)
	}

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}

	if err := store.WithTx(func(txStore *models.Store) error {
		items, err := txStore.ListAllMemories()
		if err != nil {
			return err
		}
		if len(items) != 1 {
			return fmt.Errorf("测试数据数量异常: %d", len(items))
		}
		return txStore.UpsertMemoryEmbeddings([]models.MemoryEmbedding{{
			MemoryID:    items[0].ID,
			ProjectName: items[0].ProjectName,
			Type:        items[0].Type,
			Vector:      models.EncodeVector([]float64{0.9, 0.1}),
			UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		}})
	}); err != nil {
		t.Fatalf("模拟旧版向量失败: %v", err)
	}

	result, err := service.EnsureEmbeddingsReady(ctx)
	if err != nil {
		t.Fatalf("启动校验旧版向量失败: %v", err)
	}
	if result.Changed {
		t.Fatalf("模型未变化且数量一致时不应重建: %+v", result)
	}
	if got := atomic.LoadInt64(&provider.calls); got != 1 {
		t.Fatalf("模型未变化时不应额外重建，实际次数=%d", got)
	}
}

// TestFetchMemoryEmbeddingsIsolatedByProjectName 验证向量查询会再次按项目名过滤，避免单库下跨项目误取向量。
func TestFetchMemoryEmbeddingsIsolatedByProjectName(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)
	provider := &stubEmbeddingProvider{enabled: true, model: "project-aware-model", vector: []float64{0.5, 0.5}}
	service.provider = provider

	if _, err := service.Write(ctx, "project-a", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "项目A向量",
		Tags:    []string{"A"},
		Summary: "用于验证项目隔离。",
		Context: "## Summary\n\n- 详情: A。",
	}}); err != nil {
		t.Fatalf("写入项目A记忆失败: %v", err)
	}
	if _, err := service.Write(ctx, "project-b", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "项目B向量",
		Tags:    []string{"B"},
		Summary: "用于验证项目隔离。",
		Context: "## Summary\n\n- 详情: B。",
	}}); err != nil {
		t.Fatalf("写入项目B记忆失败: %v", err)
	}

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}

	items, err := store.ListAllMemories()
	if err != nil {
		t.Fatalf("读取记忆失败: %v", err)
	}
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		rows = append(rows, memoryRowFromModel(item))
	}
	if len(rows) != 2 {
		t.Fatalf("测试数据数量异常: %d", len(rows))
	}
	allIDs := []int64{rows[0].ID, rows[1].ID}

	embeddings, err := store.ListMemoryEmbeddings("project-a", allIDs)
	if err != nil {
		t.Fatalf("按项目读取向量失败: %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("项目A查询不应读到其他项目向量: %+v", embeddings)
	}

	projectAItems, err := store.ListMemoriesByProjectAndType("project-a", "summary")
	if err != nil {
		t.Fatalf("读取项目A记忆失败: %v", err)
	}
	projectARows := make([]models.Memory, 0, len(projectAItems))
	projectARows = append(projectARows, projectAItems...)
	if len(projectARows) != 1 {
		t.Fatalf("项目A记忆数量异常: %d", len(projectARows))
	}
	if _, ok := embeddings[projectARows[0].ID]; !ok {
		t.Fatalf("项目A向量查询未返回自身记录: %+v", embeddings)
	}
}

// TestServiceSemanticSearchFindsMatchAcrossBatches 验证语义搜索会跨批次继续取候选，而不是只看第一批向量。
func TestServiceSemanticSearchFindsMatchAcrossBatches(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir(), EmbeddingConfig: &config.EmbeddingConfig{SemanticCandidateBatchSize: 256, SemanticCandidateMaxCount: 1024, SemanticHitFetchLimit: 64, SemanticSimilarityThreshold: 0.15}})
	ctx := testContextWithStore(t, service.config)
	provider := &queryEmbeddingProvider{vector: []float64{1, 0}}
	service.provider = provider

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	batchSize := service.semanticCandidateBatchSize()
	base := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	for idx := 0; idx < batchSize+24; idx++ {
		vector := []float64{0, 1}
		title := fmt.Sprintf("普通候选-%03d", idx)
		content := fmt.Sprintf("## Summary\n\n- 详情: 普通候选 %d。", idx)
		if idx == batchSize+8 {
			vector = []float64{1, 0}
			title = "跨批次目标记忆"
			content = "## Summary\n\n- 详情: 第二批候选里的真实目标。"
		}
		seedSemanticMemory(t, store, "semantic-batch-project", "summary", title, content, base.Add(-time.Duration(idx)*time.Minute).Format("20060102150405"), vector)
	}

	result, err := service.Search(ctx, "semantic-batch-project", nil, "跨批次语义查询", false)
	if err != nil {
		t.Fatalf("语义搜索失败: %v", err)
	}
	if got := atomic.LoadInt64(&provider.calls); got != 1 {
		t.Fatalf("并发搜索应复用同一组查询向量，实际次数=%d", got)
	}
	if len(result.SummaryHits) == 0 {
		t.Fatalf("语义搜索未返回跨批次候选: %+v", result.SummaryHits)
	}
	if result.SummaryHits[0].Title != "跨批次目标记忆" {
		t.Fatalf("语义搜索未命中第二批候选: %+v", result.SummaryHits)
	}
}

// TestServiceSemanticSearchFindsOlderStrongMatch 验证数据库向量检索会优先返回高相似度候选，而不是仅按时间窗口截断。
func TestServiceSemanticSearchLimitsCandidateWindow(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir(), EmbeddingConfig: &config.EmbeddingConfig{SemanticCandidateBatchSize: 256, SemanticCandidateMaxCount: 1024, SemanticHitFetchLimit: 64, SemanticSimilarityThreshold: 0.15}})
	ctx := testContextWithStore(t, service.config)
	service.provider = &queryEmbeddingProvider{vector: []float64{1, 0}}

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	maxCount := service.semanticCandidateMaxCount()
	base := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	for idx := 0; idx < maxCount+8; idx++ {
		vector := []float64{0, 1}
		title := fmt.Sprintf("窗口候选-%04d", idx)
		content := fmt.Sprintf("## Summary\n\n- 详情: 窗口候选 %d。", idx)
		if idx == maxCount+2 {
			vector = []float64{1, 0}
			title = "窗口外目标记忆"
			content = "## Summary\n\n- 详情: 超出候选窗口的语义目标。"
		}
		seedSemanticMemory(t, store, "semantic-window-project", "summary", title, content, base.Add(-time.Duration(idx)*time.Minute).Format("20060102150405"), vector)
	}

	result, err := service.Search(ctx, "semantic-window-project", nil, "窗口外语义查询", false)
	if err != nil {
		t.Fatalf("语义搜索失败: %v", err)
	}
	if len(result.SummaryHits) == 0 {
		t.Fatalf("语义搜索未返回命中: %+v", result.SummaryHits)
	}
	if result.SummaryHits[0].Title != "窗口外目标记忆" {
		t.Fatalf("高相似度候选应优先返回: %+v", result.SummaryHits)
	}
}

// TestServiceSearchLimitsLowConfidenceHits 验证低置信度结果会按配置裁剪，同时保留所有满分命中。
func TestServiceSearchLimitsLowConfidenceHits(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{
		MemoryRoot: t.TempDir(),
		SearchConfig: &config.SearchConfig{
			LowConfidenceErrorHitLimit:   1,
			LowConfidenceSummaryHitLimit: 1,
		},
	})

	now := time.Now().UTC()
	errorHits := []Hit{
		{ID: 1, Source: "error", Title: "错误满分一", Confidence: 1, Timestamp: now},
		{ID: 2, Source: "error", Title: "错误低分一", Confidence: 0.8, Timestamp: now.Add(-time.Minute)},
		{ID: 3, Source: "error", Title: "错误低分二", Confidence: 0.7, Timestamp: now.Add(-2 * time.Minute)},
		{ID: 4, Source: "error", Title: "错误满分二", Confidence: 1, Timestamp: now.Add(-3 * time.Minute)},
	}
	summaryHits := []Hit{
		{ID: 5, Source: "summary", Title: "总结低分一", Confidence: 0.9, Timestamp: now.Add(-time.Minute)},
		{ID: 6, Source: "summary", Title: "总结满分一", Confidence: 1, Timestamp: now},
		{ID: 7, Source: "summary", Title: "总结低分二", Confidence: 0.8, Timestamp: now.Add(-2 * time.Minute)},
		{ID: 8, Source: "summary", Title: "总结满分二", Confidence: 1, Timestamp: now.Add(-3 * time.Minute)},
	}

	result := SearchResult{
		Query:       "limit-test",
		ProjectName: "limit-project",
		ErrorHits:   service.limitSearchHits(errorHits, service.lowConfidenceErrorHitLimit()),
		SummaryHits: service.limitSearchHits(summaryHits, service.lowConfidenceSummaryHitLimit()),
	}

	if len(result.ErrorHits) != 3 {
		t.Fatalf("错误命中数量异常: %+v", result.ErrorHits)
	}
	if result.ErrorHits[0].Title != "错误满分一" || result.ErrorHits[1].Title != "错误低分一" || result.ErrorHits[2].Title != "错误满分二" {
		t.Fatalf("错误命中裁剪结果异常: %+v", result.ErrorHits)
	}
	if len(result.SummaryHits) != 3 {
		t.Fatalf("总结命中数量异常: %+v", result.SummaryHits)
	}
	if result.SummaryHits[0].Title != "总结低分一" || result.SummaryHits[1].Title != "总结满分一" || result.SummaryHits[2].Title != "总结满分二" {
		t.Fatalf("总结命中裁剪结果异常: %+v", result.SummaryHits)
	}
	if strings.Contains(renderResultMarkdown(result.Query, result.ProjectName, result.ErrorHits, result.SummaryHits, nil), "错误低分二") {
		t.Fatalf("被裁掉的低置信度错误命中不应出现在结果中")
	}
	if strings.Contains(renderResultMarkdown(result.Query, result.ProjectName, result.ErrorHits, result.SummaryHits, nil), "总结低分二") {
		t.Fatalf("被裁掉的低置信度总结命中不应出现在结果中")
	}
}

// TestServiceListKeepsAllSemanticHits 验证管理列表搜索不会沿用 search 接口的低置信度裁剪上限。
func TestServiceListKeepsAllSemanticHits(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{
		MemoryRoot: t.TempDir(),
		SearchConfig: &config.SearchConfig{
			LowConfidenceErrorHitLimit:   1,
			LowConfidenceSummaryHitLimit: 1,
		},
		EmbeddingConfig: &config.EmbeddingConfig{
			SemanticCandidateBatchSize:  16,
			SemanticCandidateMaxCount:   64,
			SemanticHitFetchLimit:       64,
			SemanticSimilarityThreshold: 0.15,
		},
	})
	ctx := testContextWithStore(t, service.config)
	service.provider = &queryEmbeddingProvider{vector: []float64{1, 0}}

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	base := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	for idx := 0; idx < 3; idx++ {
		seedSemanticMemory(
			t,
			store,
			fmt.Sprintf("list-semantic-project-%d", idx),
			"summary",
			fmt.Sprintf("管理列表语义记忆-%d", idx+1),
			fmt.Sprintf("## Summary\n\n- 详情: 管理列表语义候选 %d。", idx+1),
			base.Add(-time.Duration(idx+1)*24*time.Hour).Format("20060102150405"),
			[]float64{1, 0},
		)
	}

	searchResult, err := service.Search(ctx, "list-semantic-project-0", nil, "管理列表语义查询", false)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	if len(searchResult.SummaryHits) != 1 {
		t.Fatalf("Search 应继续保留低置信度裁剪: %+v", searchResult.SummaryHits)
	}

	listResult, err := service.List(ctx, 1, 10, nil, "管理列表语义查询")
	if err != nil {
		t.Fatalf("List 返回错误: %v", err)
	}
	if listResult.Total != 3 {
		t.Fatalf("List 不应裁剪语义命中: total=%d items=%+v", listResult.Total, listResult.Items)
	}
	if len(listResult.Items) != 3 {
		t.Fatalf("List 当前页结果数量异常: %+v", listResult.Items)
	}
	for _, item := range listResult.Items {
		if item.Confidence == nil || *item.Confidence <= 0 {
			t.Fatalf("List 搜索结果应返回置信度: %+v", item)
		}
	}
}

// TestServiceSemanticSearchUsesDynamicWindow 验证动态候选窗口会约束语义回表数量，避免大库查询一次返回过多低区分度结果。
func TestServiceSemanticSearchUsesDynamicWindow(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{
		MemoryRoot: t.TempDir(),
		EmbeddingConfig: &config.EmbeddingConfig{
			SemanticCandidateBatchSize:  16,
			SemanticCandidateMaxCount:   256,
			SemanticHitFetchLimit:       64,
			SemanticSimilarityThreshold: 0.15,
			SemanticWindowMode:          "dynamic",
			SemanticWindowBaseMaxCount:  32,
			SemanticWindowDynamicMin:    2,
			SemanticWindowDynamicMax:    2,
			SemanticWindowDynamicRatio:  0.01,
			DecayEnabled:                false,
		},
	})
	ctx := testContextWithStore(t, service.config)
	service.provider = &queryEmbeddingProvider{vector: []float64{1, 0}}

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	base := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	for idx := 0; idx < 10; idx++ {
		seedSemanticMemory(
			t,
			store,
			"dynamic-window-project",
			"summary",
			fmt.Sprintf("动态窗口候选-%d", idx+1),
			fmt.Sprintf("## Summary\n\n- 详情: 动态窗口候选 %d。", idx+1),
			base.Add(-time.Duration(idx)*time.Minute).Format("20060102150405"),
			[]float64{1, 0},
		)
	}

	result, err := service.Search(ctx, "dynamic-window-project", nil, "动态窗口语义查询", false)
	if err != nil {
		t.Fatalf("语义搜索失败: %v", err)
	}
	if len(result.SummaryHits) != 2 {
		t.Fatalf("动态窗口应把语义结果限制为 2 条，实际=%d", len(result.SummaryHits))
	}
}

// TestServiceConfidenceByAgeForType 验证不同记忆类型使用不同半衰期，避免错误记忆被与总结记忆相同速率衰减。
func TestServiceConfidenceByAgeForType(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{
		EmbeddingConfig: &config.EmbeddingConfig{
			DecayEnabled:             true,
			DecayAgeWeight:           1,
			DecaySemanticWeight:      0,
			DecaySummaryHalfLifeDays: 30,
			DecayErrorHalfLifeDays:   90,
		},
	})
	now := time.Now().UTC()
	ts := now.Add(-60 * 24 * time.Hour)
	summaryConfidence := service.confidenceByAgeForType("summary", ts, now)
	errorConfidence := service.confidenceByAgeForType("error", ts, now)
	if errorConfidence <= summaryConfidence {
		t.Fatalf("错误记忆衰减应慢于总结记忆: error=%v summary=%v", errorConfidence, summaryConfidence)
	}
}

// TestServiceFusedConfidenceRespectsWeights 验证融合权重会直接影响最终置信度，避免融合逻辑退化为固定策略。
func TestServiceFusedConfidenceRespectsWeights(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	keywordHit := Hit{ID: 1, Source: "summary", Confidence: 0.2, Timestamp: now}
	semanticHit := Hit{ID: 1, Source: "summary", Confidence: 0.9, Timestamp: now}

	keywordFirst := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{FusionEnabled: true, FusionFormula: "coverage_discount", FusionCoverageDiscountBase: 0.85, FusionKeywordWeight: 1, FusionSemanticWeight: 0, FusionRecencyWeight: 0}})
	if got := keywordFirst.fusedConfidence("summary", true, keywordHit, true, semanticHit, now); math.Abs(got-0.2) > 1e-9 {
		t.Fatalf("关键字优先融合结果异常: got=%v want=0.2", got)
	}

	semanticFirst := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{FusionEnabled: true, FusionFormula: "coverage_discount", FusionCoverageDiscountBase: 0.85, FusionKeywordWeight: 0, FusionSemanticWeight: 1, FusionRecencyWeight: 0}})
	if got := semanticFirst.fusedConfidence("summary", true, keywordHit, true, semanticHit, now); math.Abs(got-0.9) > 1e-9 {
		t.Fatalf("语义优先融合结果异常: got=%v want=0.9", got)
	}
}

// TestServiceMergeHitsPreservesKeywordSnippetAndAppliesFusion 验证融合后仍优先保留关键字片段，并按配置权重重算置信度。
func TestServiceMergeHitsPreservesKeywordSnippetAndAppliesFusion(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{FusionEnabled: true, FusionFormula: "coverage_discount", FusionCoverageDiscountBase: 0.85, FusionKeywordWeight: 0.8, FusionSemanticWeight: 0.2, FusionRecencyWeight: 0}})
	now := time.Now().UTC()
	keywordHits := []Hit{{
		ID:         1,
		Source:     "summary",
		Title:      "关键字命中",
		Timestamp:  now.Add(-24 * time.Hour),
		Confidence: 0.2,
		Snippets:   []Snippet{{Start: 1, End: 3, Content: "关键字片段"}},
	}}
	semanticHits := []Hit{{
		ID:          1,
		Source:      "summary",
		Title:       "语义命中",
		Timestamp:   now,
		Confidence:  0.9,
		FileContent: "语义正文",
	}}

	merged := service.mergeHits("summary", keywordHits, semanticHits)
	if len(merged) != 1 {
		t.Fatalf("融合命中数量异常: %+v", merged)
	}
	if len(merged[0].Snippets) == 0 || merged[0].Snippets[0].Content != "关键字片段" {
		t.Fatalf("融合后应保留关键字片段: %+v", merged[0])
	}
	if merged[0].FileContent != "语义正文" {
		t.Fatalf("融合后应补齐语义正文: %+v", merged[0])
	}
	want := 0.2*0.8 + 0.9*0.2
	if math.Abs(merged[0].Confidence-want) > 1e-9 {
		t.Fatalf("融合置信度异常: got=%v want=%v", merged[0].Confidence, want)
	}
}

// TestServiceMergeHitsResortsByFinalConfidence 验证融合命中会按最终分数重新排序，避免沿用旧输入顺序误伤高相关结果。
func TestServiceMergeHitsResortsByFinalConfidence(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	service := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{
		FusionEnabled:              true,
		FusionFormula:              "coverage_discount",
		FusionCoverageDiscountBase: 0.85,
		FusionKeywordWeight:        0.8,
		FusionSemanticWeight:       0.2,
		FusionRecencyWeight:        0,
	}})
	keywordHits := []Hit{
		{ID: 1, Source: "summary", Title: "旧命中", Confidence: 0.1, Timestamp: now.Add(-2 * time.Hour), Snippets: []Snippet{{Start: 1, End: 1, Content: "旧片段"}}},
		{ID: 2, Source: "summary", Title: "新命中", Confidence: 0.2, Timestamp: now.Add(-time.Hour), Snippets: []Snippet{{Start: 1, End: 1, Content: "新片段"}}},
	}
	semanticHits := []Hit{
		{ID: 1, Source: "summary", Title: "旧命中", Confidence: 0.95, Timestamp: now, FileContent: "旧正文"},
		{ID: 2, Source: "summary", Title: "新命中", Confidence: 0.3, Timestamp: now, FileContent: "新正文"},
	}
	merged := service.mergeHits("summary", keywordHits, semanticHits)
	if len(merged) != 2 {
		t.Fatalf("融合命中数量异常: %+v", merged)
	}
	if merged[0].ID != 1 || merged[1].ID != 2 {
		t.Fatalf("融合结果应按最终置信度重排: %+v", merged)
	}
	if merged[0].Confidence <= merged[1].Confidence {
		t.Fatalf("前一条命中的最终置信度应更高: %+v", merged)
	}
}

// TestServiceFusedConfidenceCoverageDiscount 提示单路命中会做轻微覆盖率折扣，避免缺失信号被直接当成 0 分拉低过多。
func TestServiceFusedConfidenceCoverageDiscount(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	service := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{
		FusionEnabled:              true,
		FusionFormula:              "coverage_discount",
		FusionCoverageDiscountBase: 0.85,
		FusionKeywordWeight:        0.55,
		FusionSemanticWeight:       0.45,
		FusionRecencyWeight:        0.1,
		FusionMinSemanticScore:     0.15,
	}})

	keywordOnly := Hit{ID: 1, Source: "summary", Confidence: 1, Timestamp: now}
	if got := service.fusedConfidence("summary", true, keywordOnly, false, Hit{}, now); math.Abs(got-0.9386363636363636) > 1e-9 {
		t.Fatalf("仅关键字命中折扣异常: got=%v want=%v", got, 0.9386363636363636)
	}

	semanticOnly := Hit{ID: 2, Source: "summary", Confidence: 0.8, Timestamp: now}
	if got := service.fusedConfidence("summary", false, Hit{}, true, semanticOnly, now); math.Abs(got-0.7736363636363637) > 1e-9 {
		t.Fatalf("仅语义命中折扣异常: got=%v want=%v", got, 0.7736363636363637)
	}
}

// TestServiceFusedConfidenceCoverageDiscountIgnoresWeakSemantic 验证弱语义命中不会参与覆盖率折扣分母，避免噪声信号稀释主命中。
func TestServiceFusedConfidenceCoverageDiscountIgnoresWeakSemantic(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	service := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{
		FusionEnabled:              true,
		FusionFormula:              "coverage_discount",
		FusionCoverageDiscountBase: 0.85,
		FusionKeywordWeight:        0.55,
		FusionSemanticWeight:       0.45,
		FusionRecencyWeight:        0.1,
		FusionMinSemanticScore:     0.15,
	}})
	keywordHit := Hit{ID: 1, Source: "summary", Confidence: 1, Timestamp: now}
	weakSemantic := Hit{ID: 1, Source: "summary", Confidence: 0.1, Timestamp: now}
	got := service.fusedConfidence("summary", true, keywordHit, true, weakSemantic, now)
	want := 0.9386363636363636
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("弱语义不应参与覆盖率折扣: got=%v want=%v", got, want)
	}
}

// TestServiceKeywordQueriesExpandsSynonyms 验证同义词扩展受配置控制，避免关键字召回依赖调用方手工补齐词表。
func TestServiceKeywordQueriesExpandsSynonyms(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{KeywordSynonymsEnabled: true, KeywordSynonymGroups: [][]string{{"error", "故障", "失败"}}}})
	queries := service.keywordQueries([]string{"error"})
	joined := strings.Join(queries, ",")
	if !strings.Contains(joined, "error") || !strings.Contains(joined, "故障") || !strings.Contains(joined, "失败") {
		t.Fatalf("同义词扩展结果异常: %+v", queries)
	}
}

// TestServiceRankRowsByKeywordBM25 验证 BM25 模式会按字段权重重排结果，避免关键字命中长期只看时间顺序。
func TestServiceRankRowsByKeywordBM25(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{SearchConfig: &config.SearchConfig{
		KeywordMode:         "bm25",
		KeywordFields:       []string{"title", "content"},
		KeywordFieldWeights: map[string]float64{"title": 3, "content": 1},
		KeywordBM25K1:       1.2,
		KeywordBM25B:        0.75,
	}})
	rows := []Row{
		{ID: 1, Title: "数据库连接池抖动", Content: "普通内容", Timestamp: "20260310100000"},
		{ID: 2, Title: "普通标题", Content: "数据库连接池出现抖动并持续重试", Timestamp: "20260310100100"},
	}
	sortedRows, scoreByID := service.rankRowsByKeywordBM25(rows, []string{"数据库连接池"})
	if len(sortedRows) != 2 {
		t.Fatalf("BM25 重排结果数量异常: %+v", sortedRows)
	}
	if sortedRows[0].ID != 1 {
		t.Fatalf("标题高权重场景下应优先返回标题命中: %+v", sortedRows)
	}
	if scoreByID[1] <= scoreByID[2] {
		t.Fatalf("标题高权重场景下得分应更高: %+v", scoreByID)
	}
}

// TestServiceSemanticSearchUsesCache 验证开启缓存后重复搜索会复用语义结果，避免重复请求嵌入服务。
func TestServiceSemanticSearchUsesCache(t *testing.T) {
	t.Helper()
	provider := &queryEmbeddingProvider{vector: []float64{1, 0}}
	service := NewService(config.AppConfig{
		MemoryRoot: t.TempDir(),
		SearchConfig: &config.SearchConfig{
			CacheEnabled:           true,
			CacheQueryEmbeddingTTL: 600,
			CacheSemanticHitsTTL:   600,
			CacheMaxEntries:        100,
		},
	})
	service.provider = provider
	ctx := testContextWithStore(t, service.config)

	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	seedSemanticMemory(t, store, "cache-project", "summary", "缓存记忆", "## Summary\n\n- 详情: 缓存命中。", "20260310100000", []float64{1, 0})

	if _, err := service.Search(ctx, "cache-project", nil, "缓存查询", false); err != nil {
		t.Fatalf("首次搜索失败: %v", err)
	}
	if _, err := service.Search(ctx, "cache-project", nil, "缓存查询", false); err != nil {
		t.Fatalf("第二次搜索失败: %v", err)
	}
	if got := atomic.LoadInt64(&provider.calls); got != 1 {
		t.Fatalf("缓存生效后应只在首轮调用一次嵌入，实际=%d", got)
	}
}

// TestServiceWriteInvalidatesSearchCache 验证写入后会清理语义缓存，避免新记忆无法被同查询及时命中。
func TestServiceWriteInvalidatesSearchCache(t *testing.T) {
	t.Helper()
	provider := &stubEmbeddingProvider{enabled: true, model: "cache-test-model", vector: []float64{1, 0}}
	service := NewService(config.AppConfig{
		MemoryRoot: t.TempDir(),
		SearchConfig: &config.SearchConfig{
			CacheEnabled:           true,
			CacheQueryEmbeddingTTL: 600,
			CacheSemanticHitsTTL:   600,
			CacheMaxEntries:        100,
		},
	})
	service.provider = provider
	ctx := testContextWithStore(t, service.config)

	if _, err := service.Write(ctx, "cache-write-project", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "缓存写入一",
		Tags:    []string{"缓存"},
		Summary: "首条缓存记忆",
		Context: "## Summary\n\n- 详情: 第一条。",
	}}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	first, err := service.Search(ctx, "cache-write-project", nil, "缓存写入", false)
	if err != nil {
		t.Fatalf("首次搜索失败: %v", err)
	}
	if len(first.SummaryHits) == 0 {
		t.Fatalf("首次搜索应命中至少一条结果: %+v", first.SummaryHits)
	}

	if _, err := service.Write(ctx, "cache-write-project", "", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "缓存写入二",
		Tags:    []string{"缓存"},
		Summary: "第二条缓存记忆",
		Context: "## Summary\n\n- 详情: 第二条。",
	}}); err != nil {
		t.Fatalf("第二次写入失败: %v", err)
	}

	second, err := service.Search(ctx, "cache-write-project", nil, "缓存写入", false)
	if err != nil {
		t.Fatalf("第二次搜索失败: %v", err)
	}
	if len(second.SummaryHits) < 2 {
		t.Fatalf("写入后应失效缓存并返回新结果: %+v", second.SummaryHits)
	}
}

// TestServiceSearchIncrementsUseCount 验证搜索最终返回结果后会累计使用次数并刷新最近使用时间。
func TestServiceSearchIncrementsUseCount(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)
	if _, err := service.Write(ctx, "usage-project", "feature/usage", "tester", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "使用计数测试",
		Tags:    []string{"统计"},
		Summary: "验证搜索会累计使用次数。",
		Context: "## Summary\n\n- 详情: 使用计数测试。",
	}}); err != nil {
		t.Fatalf("写入测试记忆失败: %v", err)
	}
	if _, err := service.Search(ctx, "usage-project", nil, "使用计数测试", false); err != nil {
		t.Fatalf("首次搜索失败: %v", err)
	}
	if _, err := service.Search(ctx, "usage-project", nil, "使用计数测试", false); err != nil {
		t.Fatalf("第二次搜索失败: %v", err)
	}
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	items, err := store.ListAllMemories()
	if err != nil || len(items) != 1 {
		t.Fatalf("读取记忆失败: err=%v items=%+v", err, items)
	}
	if items[0].UseCount != 2 {
		t.Fatalf("UseCount = %d, want 2", items[0].UseCount)
	}
	if strings.TrimSpace(items[0].LastUsedAt) == "" {
		t.Fatalf("LastUsedAt 不应为空: %+v", items[0])
	}
}

// TestServiceEnsureProtectedTagsSeeded 验证配置中的保护标签种子会自动补齐到数据库，避免后台列表首次启动为空。
func TestServiceEnsureProtectedTagsSeeded(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir(), ScheduleConfig: &config.ScheduleConfig{MemoryCleanup: config.MemoryCleanupScheduleConfig{ProtectedTags: []string{"核心故障", "架构决策"}}}})
	ctx := testContextWithStore(t, service.config)
	if err := service.EnsureProtectedTagsSeeded(ctx); err != nil {
		t.Fatalf("补种保护标签失败: %v", err)
	}
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	items, err := store.ListEnabledProtectedTags()
	if err != nil {
		t.Fatalf("读取保护标签失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("保护标签数量异常: %+v", items)
	}
}

// TestServiceRunMemoryCleanupOnceBuildsReviews 验证清理任务会跳过保护标签，并只把高分候选写入待审核队列。
func TestServiceRunMemoryCleanupOnceBuildsReviews(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir(), ScheduleConfig: &config.ScheduleConfig{MemoryCleanup: config.MemoryCleanupScheduleConfig{
		Mode:       "review",
		ReviewTopN: 20,
		Summary:    config.MemoryCleanupPolicyConfig{BeforeDays: 1, BatchSize: 10, MinRemaining: 0, MinRemainingPerProject: 0, ScoreThreshold: 0, Weights: config.MemoryCleanupWeightsConfig{Age: 0.3, UseCount: 0.35, LastUsed: 0.25, ProjectPressure: 0.1}},
		Error:      config.MemoryCleanupPolicyConfig{BeforeDays: 1, BatchSize: 10, MinRemaining: 0, MinRemainingPerProject: 0, ScoreThreshold: 0, Weights: config.MemoryCleanupWeightsConfig{Age: 0.2, UseCount: 0.25, LastUsed: 0.35, ProjectPressure: 0.2}},
	}}})
	ctx := testContextWithStore(t, service.config)
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	now := time.Now().UTC()
	protected, err := store.CreateProtectedTag(models.MemoryProtectedTag{Tag: "核心故障", Enabled: true, Source: "manual", CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatalf("创建保护标签失败: %v", err)
	}
	_ = protected
	items, err := store.CreateMemories([]models.Memory{
		{UserID: "u1", ProjectName: "cleanup-project", GitBranch: "main", Type: "summary", Title: "应进入候选", Tags: models.EncodeTags([]string{"普通"}), Summary: "普通候选", Content: "内容", UseCount: 0, LastUsedAt: now.AddDate(0, 0, -10).Format(time.RFC3339Nano), Timestamp: now.AddDate(0, 0, -10).Format("20060102150405"), CreatedAt: now.AddDate(0, 0, -10).Format(time.RFC3339Nano)},
		{UserID: "u2", ProjectName: "cleanup-project", GitBranch: "main", Type: "summary", Title: "受保护候选", Tags: models.EncodeTags([]string{"核心故障"}), Summary: "受保护候选", Content: "内容", UseCount: 0, LastUsedAt: now.AddDate(0, 0, -10).Format(time.RFC3339Nano), Timestamp: now.AddDate(0, 0, -10).Format("20060102150405"), CreatedAt: now.AddDate(0, 0, -10).Format(time.RFC3339Nano)},
	})
	if err != nil {
		t.Fatalf("写入候选记忆失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("候选记忆数量异常: %+v", items)
	}
	result, err := service.RunMemoryCleanupOnce(ctx)
	if err != nil {
		t.Fatalf("执行清理任务失败: %v", err)
	}
	if result.SummaryCandidateCount != 1 {
		t.Fatalf("summary 候选数量异常: %+v", result)
	}
	reviews, total, err := store.ListCleanupReviews("pending", "summary", "", 1, 10)
	if err != nil {
		t.Fatalf("读取审核列表失败: %v", err)
	}
	if total != 1 || len(reviews) != 1 {
		t.Fatalf("审核记录数量异常: total=%d items=%+v", total, reviews)
	}
	if reviews[0].MemoryID != items[0].ID {
		t.Fatalf("保护标签命中的记忆不应进入候选: %+v", reviews)
	}
}

// TestExecuteApprovedCleanupReviewsOnlySelected 验证执行清理时只会删除前端勾选且已批准的审核记录。
func TestExecuteApprovedCleanupReviewsOnlySelected(t *testing.T) {
	t.Helper()
	service := NewService(config.AppConfig{MemoryRoot: t.TempDir()})
	ctx := testContextWithStore(t, service.config)
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("读取模型存储失败: %v", err)
	}
	now := time.Now().UTC()
	items, err := store.CreateMemories([]models.Memory{
		{UserID: "u1", ProjectName: "cleanup-project", GitBranch: "main", Type: "summary", Title: "选中删除", Tags: models.EncodeTags([]string{"普通"}), Summary: "待删", Content: "内容", Timestamp: now.AddDate(0, 0, -10).Format("20060102150405"), CreatedAt: now.AddDate(0, 0, -10).Format(time.RFC3339Nano)},
		{UserID: "u2", ProjectName: "cleanup-project", GitBranch: "main", Type: "summary", Title: "未选中保留", Tags: models.EncodeTags([]string{"普通"}), Summary: "保留", Content: "内容", Timestamp: now.AddDate(0, 0, -11).Format("20060102150405"), CreatedAt: now.AddDate(0, 0, -11).Format(time.RFC3339Nano)},
	})
	if err != nil {
		t.Fatalf("写入测试记忆失败: %v", err)
	}
	createdAt := now.Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(createdAt, "summary", []models.MemoryCleanupReview{{MemoryID: items[0].ID, ProjectName: items[0].ProjectName, Type: "summary", Status: "pending", Score: 0.9, RunAt: createdAt, CreatedAt: createdAt}}); err != nil {
		t.Fatalf("写入审核记录A失败: %v", err)
	}
	if err := store.ReplacePendingCleanupReviews(createdAt+"-b", "summary", []models.MemoryCleanupReview{{MemoryID: items[1].ID, ProjectName: items[1].ProjectName, Type: "summary", Status: "pending", Score: 0.8, RunAt: createdAt + "-b", CreatedAt: createdAt}}); err != nil {
		t.Fatalf("写入审核记录B失败: %v", err)
	}
	reviews, total, err := store.ListCleanupReviews("pending", "summary", "", 1, 10)
	if err != nil || total != 2 {
		t.Fatalf("读取审核记录失败: err=%v total=%d items=%+v", err, total, reviews)
	}
	selectedReviewID := reviews[0].ID
	if reviews[0].MemoryID != items[0].ID {
		selectedReviewID = reviews[1].ID
	}
	if err := service.ApproveCleanupReviews(ctx, []int64{selectedReviewID}, "tester"); err != nil {
		t.Fatalf("批准审核记录失败: %v", err)
	}
	result, err := service.ExecuteApprovedCleanupReviews(ctx, []int64{selectedReviewID}, 1, "tester")
	if err != nil {
		t.Fatalf("执行选中审核记录失败: %v", err)
	}
	if result.ExecutedReviewCount != 1 || result.ExecutedMemoryCount != 1 {
		t.Fatalf("执行结果异常: %+v", result)
	}
	remaining, err := store.ListAllMemories()
	if err != nil {
		t.Fatalf("读取剩余记忆失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Title != "未选中保留" {
		t.Fatalf("未选中的审核记录不应被删除: %+v", remaining)
	}
}
