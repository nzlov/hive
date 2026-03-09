package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/memory"
	"github.com/nzlov/hive/internal/user"
)

// TestRouterWriteAndSearch 验证 HTTP 路由能正确透传到服务层，避免接口协议改动后脚本调用失效。
func TestRouterWriteAndSearch(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{
		MemoryRoot:       memoryRoot,
		ServerBaseURL:    "http://127.0.0.1:19090",
		ServerListenAddr: ":19090",
		JWTSecret:        "test-secret",
	})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := NewRouter(service, userService)
	writeBody, err := json.Marshal(api.WriteRequest{ProjectName: "router-alias", GitBranch: "feature/router", Items: []api.MemoryWriteItem{{
		Type:    "error",
		Title:   "HTTP接口测试",
		Tags:    []string{"HTTP", "测试"},
		Summary: "验证 HTTP 写入接口。",
		Context: "## Summary\n\n- 详情: 通过 HTTP 接口写入错误记忆。",
	}}})
	if err != nil {
		t.Fatalf("构造写入请求失败: %v", err)
	}
	writeRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/write", bytes.NewReader(writeBody))
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRequest.Header.Set("X-API-Token", admin.APIToken)
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("写入接口返回状态异常: %d, body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}

	searchBody, err := json.Marshal(api.SearchRequest{ProjectName: "router-alias", Queries: []string{"HTTP接口测试"}, Debug: false})
	if err != nil {
		t.Fatalf("构造搜索请求失败: %v", err)
	}
	searchRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/search", bytes.NewReader(searchBody))
	searchRequest.Header.Set("Content-Type", "application/json")
	searchRequest.Header.Set("X-API-Token", admin.APIToken)
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
	if len(searchResponse.ErrorHits) != 1 || searchResponse.ErrorHits[0].ProjectName != "router-alias" {
		t.Fatalf("搜索响应未返回项目名: %+v", searchResponse.ErrorHits)
	}
	if searchResponse.ErrorHits[0].GitBranch != "feature/router" {
		t.Fatalf("搜索响应未返回 git 分支: %+v", searchResponse.ErrorHits[0])
	}
	if searchResponse.Error != "" {
		t.Fatalf("成功响应不应返回 error 字段内容: %+v", searchResponse)
	}
}

// TestRouterReturnsErrorField 验证接口失败时会通过统一 error 字段返回错误，避免客户端继续依赖非结构化响应。
func TestRouterReturnsErrorField(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{
		MemoryRoot:       memoryRoot,
		ServerBaseURL:    "http://127.0.0.1:19090",
		ServerListenAddr: ":19090",
		JWTSecret:        "test-secret",
	})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := NewRouter(service, userService)

	searchRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/search", bytes.NewReader([]byte(`{"project_name":123}`)))
	searchRequest.Header.Set("Content-Type", "application/json")
	searchRequest.Header.Set("X-API-Token", admin.APIToken)
	searchRecorder := httptest.NewRecorder()
	router.ServeHTTP(searchRecorder, searchRequest)
	if searchRecorder.Code != http.StatusBadRequest {
		t.Fatalf("错误请求返回状态异常: %d, body=%s", searchRecorder.Code, searchRecorder.Body.String())
	}
	var searchResponse api.SearchResponse
	if err := json.Unmarshal(searchRecorder.Body.Bytes(), &searchResponse); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if strings.TrimSpace(searchResponse.Error) == "" {
		t.Fatalf("错误响应未返回 error 字段: %+v", searchResponse)
	}
}

// TestRouterDoesNotExposeRebuildEmbeddingsEndpoint 验证向量重建不再通过 HTTP 暴露，避免维护入口与启动自愈逻辑并存。
func TestRouterDoesNotExposeRebuildEmbeddingsEndpoint(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	router := NewRouter(service, userService)

	req := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/rebuild-embeddings", bytes.NewReader([]byte(`{"force":true}`)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("重建接口应已移除: status=%d, body=%s", recorder.Code, recorder.Body.String())
	}
}

// TestRouterLoginAndUserList 验证管理端登录和 JWT 鉴权链路可用，避免前端管理页无法获取用户列表。
func TestRouterLoginAndUserList(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := NewRouter(service, userService)

	loginBody := bytes.NewReader([]byte(`{"username":"admin","password":"` + password + `"}`))
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("登录接口返回状态异常: %d, body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse api.LoginResponse
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	if strings.TrimSpace(loginResponse.Token) == "" {
		t.Fatalf("登录响应未返回 JWT: %+v", loginResponse)
	}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	listRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("用户列表接口返回状态异常: %d, body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.UserListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析用户列表失败: %v", err)
	}
	if len(listResponse.Items) == 0 || listResponse.Items[0].Username == "" {
		t.Fatalf("用户列表为空: %+v", listResponse)
	}
}
