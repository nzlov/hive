package memory

import (
	"strings"
	"testing"

	"memory-manager/internal/api"
	"memory-manager/internal/config"
)

// TestServiceWriteAndSearch 验证服务层可以完成写入和检索，避免 HTTP 之下的核心流程回归失效。
func TestServiceWriteAndSearch(t *testing.T) {
	t.Helper()
	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	service := NewService(config.AppConfig{
		StorageRoot:      storageRoot,
		ServerBaseURL:    "http://127.0.0.1:18080",
		ServerListenAddr: ":18080",
	})

	databasePath, err := service.Write(projectRoot, "service-alias", "feature/test-branch", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "服务层写入测试",
		Tags:    []string{"服务层", "测试"},
		Summary: "验证服务层写入后能够被检索命中。",
		Context: "## Summary\n\n- 详情: 服务层测试写入内容。",
	}})
	if err != nil {
		t.Fatalf("Write 返回错误: %v", err)
	}
	if !strings.HasSuffix(databasePath, ".memory/memory.db") {
		t.Fatalf("Write 返回的数据库路径不符合预期: %s", databasePath)
	}

	result, err := service.Search(projectRoot, "service-alias", []string{"服务层写入测试"}, true)
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
	if len(result.SummaryHits) != 1 || result.SummaryHits[0].ProjectName != "service-alias" {
		t.Fatalf("Search 结果未按显式项目名返回: %+v", result.SummaryHits)
	}
	if result.SummaryHits[0].GitBranch != "feature/test-branch" {
		t.Fatalf("Search 结果未返回 git 分支: %+v", result.SummaryHits[0])
	}
}

// TestServiceSearchReturnsBranchMetadata 验证查询会返回分支元信息，后续由脚本决定是否保留该条记忆。
func TestServiceSearchReturnsBranchMetadata(t *testing.T) {
	t.Helper()
	projectRoot := t.TempDir()
	storageRoot := t.TempDir()
	service := NewService(config.AppConfig{StorageRoot: storageRoot})

	if _, err := service.Write(projectRoot, "branch-project", "feature/a", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "A分支记忆",
		Tags:    []string{"分支"},
		Summary: "仅 feature/a 可见。",
		Context: "## Summary\n\n- 详情: A 分支的结论。",
	}}); err != nil {
		t.Fatalf("写入 feature/a 记忆失败: %v", err)
	}
	if _, err := service.Write(projectRoot, "branch-project", "", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "公共记忆",
		Tags:    []string{"公共"},
		Summary: "所有分支都可见。",
		Context: "## Summary\n\n- 详情: 公共记忆。",
	}}); err != nil {
		t.Fatalf("写入公共记忆失败: %v", err)
	}

	result, err := service.Search(projectRoot, "branch-project", []string{"记忆"}, false)
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
