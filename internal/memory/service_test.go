package memory

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

// stubEmbeddingProvider 伪造稳定向量返回，避免测试依赖外部嵌入服务可用性。
type stubEmbeddingProvider struct {
	enabled bool
	model   string
	vector  []float64
	calls   int
}

// Enabled 让测试显式控制当前 provider 是否启用，避免不同分支隐式耦合。
func (p *stubEmbeddingProvider) Enabled() bool { return p.enabled }

// ModelName 返回固定模型名，方便验证模型切换后的元数据是否同步更新。
func (p *stubEmbeddingProvider) ModelName() string { return p.model }

// EmbedTexts 为每条输入返回同一组向量，让测试只关注重建触发条件而非算法细节。
func (p *stubEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) {
	p.calls++
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

	result, err := service.Search(ctx, "service-alias", []string{"服务层写入测试"}, true)
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

	result, err := service.Search(ctx, "branch-project", []string{"记忆"}, false)
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

	result, err := service.Search(ctx, "snippet-project", []string{"长连接池"}, false)
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

	resultA, err := service.Search(ctx, "project-a", []string{"记忆"}, false)
	if err != nil {
		t.Fatalf("搜索项目A记忆失败: %v", err)
	}
	if len(resultA.SummaryHits) != 1 {
		t.Fatalf("项目A搜索结果未正确隔离: %+v", resultA.SummaryHits)
	}
	if strings.Contains(resultA.Markdown(), "项目B记忆") {
		t.Fatalf("项目A搜索结果串入了项目B记忆: %s", resultA.Markdown())
	}

	resultB, err := service.Search(ctx, "project-b", []string{"记忆"}, false)
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
	if oldProvider.calls != 1 {
		t.Fatalf("旧模型写入时应生成一次向量，实际次数=%d", oldProvider.calls)
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
	if newProvider.calls != 1 {
		t.Fatalf("模型切换后应执行一次重建，实际次数=%d", newProvider.calls)
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
