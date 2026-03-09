package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memory-manager/internal/api"
	"memory-manager/internal/config"
	"memory-manager/internal/memory"
)

// TestRouterWriteAndSearch 验证 HTTP 路由能正确透传到服务层，避免接口协议改动后脚本调用失效。
func TestRouterWriteAndSearch(t *testing.T) {
	t.Helper()
	service := memory.NewService(config.AppConfig{
		StorageRoot:      t.TempDir(),
		ServerBaseURL:    "http://127.0.0.1:19090",
		ServerListenAddr: ":19090",
	})
	router := NewRouter(service)
	projectRoot := t.TempDir()

	writeBody, err := json.Marshal(api.WriteRequest{ProjectRoot: projectRoot, Items: []api.MemoryWriteItem{{
		Type:    "error",
		Title:   "HTTP接口测试",
		Tags:    []string{"HTTP", "测试"},
		Summary: "验证 HTTP 写入接口。",
		Context: "## Summary\n\n- 详情: 通过 HTTP 接口写入错误记忆。",
	}}})
	if err != nil {
		t.Fatalf("构造写入请求失败: %v", err)
	}
	writeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/memories/write", bytes.NewReader(writeBody))
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("写入接口返回状态异常: %d, body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}

	searchBody, err := json.Marshal(api.SearchRequest{ProjectRoot: projectRoot, Queries: []string{"HTTP接口测试"}, Debug: false})
	if err != nil {
		t.Fatalf("构造搜索请求失败: %v", err)
	}
	searchRequest := httptest.NewRequest(http.MethodPost, "/api/v1/memories/search", bytes.NewReader(searchBody))
	searchRequest.Header.Set("Content-Type", "application/json")
	searchRecorder := httptest.NewRecorder()
	router.ServeHTTP(searchRecorder, searchRequest)
	if searchRecorder.Code != http.StatusOK {
		t.Fatalf("搜索接口返回状态异常: %d, body=%s", searchRecorder.Code, searchRecorder.Body.String())
	}
	var searchResponse api.SearchResponse
	if err := json.Unmarshal(searchRecorder.Body.Bytes(), &searchResponse); err != nil {
		t.Fatalf("解析搜索响应失败: %v", err)
	}
	if !strings.Contains(searchResponse.Markdown, "Error Hits (1)") {
		t.Fatalf("搜索结果未命中错误记忆: %s", searchResponse.Markdown)
	}
	if !strings.Contains(searchResponse.Markdown, "HTTP接口测试") {
		t.Fatalf("搜索结果缺少写入标题: %s", searchResponse.Markdown)
	}
}
