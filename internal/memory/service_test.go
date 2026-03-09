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

	databasePath, err := service.Write(projectRoot, []api.MemoryWriteItem{{
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

	markdown, err := service.Search(projectRoot, []string{"服务层写入测试"}, true)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	if !strings.Contains(markdown, "Summary Hits (1)") {
		t.Fatalf("Search 结果未包含总结命中: %s", markdown)
	}
	if !strings.Contains(markdown, "服务层写入测试") {
		t.Fatalf("Search 结果未包含写入标题: %s", markdown)
	}
	if !strings.Contains(markdown, "Debug Commands") {
		t.Fatalf("Search 调试输出缺少 Debug Commands: %s", markdown)
	}
}
