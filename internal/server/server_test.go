package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/memory"
	"github.com/nzlov/hive/internal/models"
	"github.com/nzlov/hive/internal/user"
)

// testContextWithStore 为路由测试准备共享 Store，确保中间件与服务层使用同一连接实例。
func testContextWithStore(t *testing.T, cfg config.AppConfig) (context.Context, *models.Store) {
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
	return models.StoreToContext(context.Background(), store), store
}

// newTestRouter 统一构造带统计服务的路由，避免每个用例重复拼装启动依赖。
func newTestRouter(t *testing.T, service *memory.Service, userService *user.Service, store *models.Store) *gin.Engine {
	t.Helper()
	statsService := NewDashboardStatsService()
	if err := statsService.Bootstrap(store); err != nil {
		t.Fatalf("初始化统计服务失败: %v", err)
	}
	return NewRouter(service, userService, store, statsService)
}

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
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
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

	searchBody, err := json.Marshal(api.SearchRequest{ProjectName: "router-alias", Tags: []string{"HTTP", "测试"}, Description: "HTTP接口测试", Debug: false})
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
	if len(searchResponse.ErrorHits) != 1 {
		t.Fatalf("搜索响应命中数量异常: %+v", searchResponse.ErrorHits)
	}
	hitsPayload, err := json.Marshal(struct {
		ErrorHits   []api.SearchHit `json:"error_hits"`
		SummaryHits []api.SearchHit `json:"summary_hits"`
	}{
		ErrorHits:   searchResponse.ErrorHits,
		SummaryHits: searchResponse.SummaryHits,
	})
	if err != nil {
		t.Fatalf("序列化命中结果失败: %v", err)
	}
	if bytes.Contains(hitsPayload, []byte(`"path"`)) {
		t.Fatalf("搜索命中结果不应再暴露 path 字段: %s", string(hitsPayload))
	}
	if bytes.Contains(hitsPayload, []byte(`"project_name"`)) {
		t.Fatalf("搜索命中结果不应再暴露 project_name 字段: %s", string(hitsPayload))
	}
	if searchResponse.ErrorHits[0].GitBranch != "feature/router" {
		t.Fatalf("搜索响应未返回 git 分支: %+v", searchResponse.ErrorHits[0])
	}
	if searchResponse.ErrorHits[0].Title != "HTTP接口测试" {
		t.Fatalf("搜索响应未返回标题: %+v", searchResponse.ErrorHits[0])
	}
	if strings.Join(searchResponse.ErrorHits[0].Tags, ",") != "HTTP,测试" {
		t.Fatalf("搜索响应未返回标签: %+v", searchResponse.ErrorHits[0])
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
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

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

// TestRouterRejectsMissingDescriptionSearch 验证搜索接口缺少 description 时会显式报错，避免请求语义不完整。
func TestRouterRejectsMissingDescriptionSearch(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

	searchRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/search", bytes.NewReader([]byte(`{"project_name":"router-alias","tags":["旧协议"]}`)))
	searchRequest.Header.Set("Content-Type", "application/json")
	searchRequest.Header.Set("X-API-Token", admin.APIToken)
	searchRecorder := httptest.NewRecorder()
	router.ServeHTTP(searchRecorder, searchRequest)
	if searchRecorder.Code != http.StatusBadRequest {
		t.Fatalf("缺少 description 的搜索请求应返回 400: %d, body=%s", searchRecorder.Code, searchRecorder.Body.String())
	}
	var searchResponse api.SearchResponse
	if err := json.Unmarshal(searchRecorder.Body.Bytes(), &searchResponse); err != nil {
		t.Fatalf("解析搜索错误响应失败: %v", err)
	}
	if !strings.Contains(searchResponse.Error, "搜索描述不能为空") {
		t.Fatalf("缺少 description 的搜索错误提示异常: %+v", searchResponse)
	}
}

// TestRouterDoesNotExposeRebuildEmbeddingsEndpoint 验证向量重建不再通过 HTTP 暴露，避免维护入口与启动自愈逻辑并存。
func TestRouterDoesNotExposeRebuildEmbeddingsEndpoint(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	router := newTestRouter(t, service, userService, store)

	req := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/rebuild-embeddings", bytes.NewReader([]byte(`{"force":true}`)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("重建接口应已移除: status=%d, body=%s", recorder.Code, recorder.Body.String())
	}
}

// TestRouterMemoryListReturnsConfidence 验证管理端记忆列表在搜索模式下会返回置信度，便于前端解释排序依据。
func TestRouterMemoryListReturnsConfidence(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	if _, err := service.Write(ctx, "router-list-project", "feature/admin", "admin", []api.MemoryWriteItem{{
		Type:    "summary",
		Title:   "管理列表关键字命中",
		Tags:    []string{"后台", "列表"},
		Summary: "用于验证管理列表的置信度展示。",
		Context: "## Summary\n\n- 详情: 管理列表关键字命中内容。",
	}}); err != nil {
		t.Fatalf("写入测试记忆失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

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

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/memories?page=1&page_size=10&description=管理列表关键字命中&tags=后台&tags=列表", nil)
	listRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("记忆列表接口返回状态异常: %d, body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.MemoryListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析记忆列表响应失败: %v", err)
	}
	if len(listResponse.Items) != 1 {
		t.Fatalf("记忆列表结果数量异常: %+v", listResponse)
	}
	if listResponse.Items[0].Type != "summary" {
		t.Fatalf("记忆列表结果未返回类型字段: %+v", listResponse.Items[0])
	}
	if listResponse.Items[0].Confidence == nil || *listResponse.Items[0].Confidence <= 0 {
		t.Fatalf("记忆列表搜索结果应返回置信度: %+v", listResponse.Items[0])
	}
}

// TestRouterRejectsMissingDescriptionList 验证管理端列表接口缺少 description 时会显式报错，避免请求语义不完整。
func TestRouterRejectsMissingDescriptionList(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

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

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/memories?page=1&page_size=10&tags=旧协议", nil)
	listRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusBadRequest {
		t.Fatalf("缺少 description 的列表请求应返回 400: %d, body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.MemoryListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析列表错误响应失败: %v", err)
	}
	if !strings.Contains(listResponse.Error, "description 不能为空") {
		t.Fatalf("缺少 description 的列表错误提示异常: %+v", listResponse)
	}
}

// TestRouterLoginAndUserList 验证管理端登录和 JWT 鉴权链路可用，避免前端管理页无法获取用户列表。
func TestRouterLoginAndUserList(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	if _, err := userService.CreateUser(ctx, user.CreateInput{Username: "alice", RealName: "爱丽丝", Password: "secret-1", IsAdmin: false}); err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	if _, err := userService.CreateUser(ctx, user.CreateInput{Username: "bob", RealName: "鲍勃", Password: "secret-2", IsAdmin: false}); err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

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
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?page=1&page_size=1&keyword=alice", nil)
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
	if listResponse.Page != 1 || listResponse.PageSize != 1 {
		t.Fatalf("用户列表分页信息异常: %+v", listResponse)
	}
	if listResponse.Total < 1 || listResponse.TotalPage < 1 {
		t.Fatalf("用户列表总数信息异常: %+v", listResponse)
	}
	if len(listResponse.Items) != 1 || listResponse.Items[0].Username != "alice" {
		t.Fatalf("用户列表为空: %+v", listResponse)
	}
}

// TestRouterMemoryEndpointsReturnCreatorName 验证记忆列表和详情直接返回创建人真实姓名，避免前端再次查询用户表。
func TestRouterMemoryEndpointsReturnCreatorName(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

	writeBody := bytes.NewReader([]byte(`{"project_name":"router-memory","git_branch":"main","items":[{"type":"summary","title":"创建人映射","tags":["creator"],"summary":"验证后端直接返回真实姓名","context":"content"}]}`))
	writeRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/write", writeBody)
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRequest.Header.Set("X-API-Token", admin.APIToken)
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("写入记忆失败: status=%d body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}

	memories, err := store.ListAllMemories()
	if err != nil || len(memories) == 0 {
		t.Fatalf("读取记忆失败: err=%v items=%+v", err, memories)
	}
	memoryID := memories[0].ID

	loginBody := bytes.NewReader([]byte(`{"username":"admin","password":"` + password + `"}`))
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("登录失败: status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse api.LoginResponse
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/memories?page=1&page_size=10&description=创建人映射&tags=creator", nil)
	listRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("记忆列表失败: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.MemoryListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析记忆列表失败: %v", err)
	}
	if len(listResponse.Items) == 0 || listResponse.Items[0].CreatorName != admin.RealName {
		t.Fatalf("记忆列表未返回创建人真实姓名: %+v", listResponse)
	}
	if listResponse.Items[0].Type != "summary" {
		t.Fatalf("记忆列表未返回记忆类型: %+v", listResponse.Items[0])
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/memories/"+strconv.FormatInt(memoryID, 10), nil)
	detailRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	detailRecorder := httptest.NewRecorder()
	router.ServeHTTP(detailRecorder, detailRequest)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("记忆详情失败: status=%d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	var detailResponse api.MemoryDetailResponse
	if err := json.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse); err != nil {
		t.Fatalf("解析记忆详情失败: %v", err)
	}
	if detailResponse.Item.CreatorName != admin.RealName {
		t.Fatalf("记忆详情未返回创建人真实姓名: %+v", detailResponse)
	}
}

// TestRouterStatsEndpoints 验证总览统计和缓存统计接口可用，避免前端新增卡片后缺少数据来源。
func TestRouterStatsEndpoints(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{
		MemoryRoot: memoryRoot,
		JWTSecret:  "test-secret",
		SearchConfig: &config.SearchConfig{
			CacheEnabled:              true,
			CacheQueryEmbeddingTTL:    600,
			CacheSemanticHitsTTL:      120,
			CacheMaxEntries:           100,
			CacheStatsRefreshInterval: 5,
		},
	})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

	writeBody := bytes.NewReader([]byte(`{"project_name":"stats-project","git_branch":"main","items":[{"type":"summary","title":"统计一","tags":["高频","后端"],"summary":"统计测试","context":"content"},{"type":"error","title":"统计二","tags":["高频"],"summary":"统计测试","context":"content"}]}`))
	writeRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/write", writeBody)
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRequest.Header.Set("X-API-Token", admin.APIToken)
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("写入记忆失败: status=%d body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}

	loginBody := bytes.NewReader([]byte(`{"username":"admin","password":"` + password + `"}`))
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("登录失败: status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse api.LoginResponse
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}

	statsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/stats", nil)
	statsRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	statsRecorder := httptest.NewRecorder()
	router.ServeHTTP(statsRecorder, statsRequest)
	if statsRecorder.Code != http.StatusOK {
		t.Fatalf("统计接口失败: status=%d body=%s", statsRecorder.Code, statsRecorder.Body.String())
	}
	var statsResponse api.DashboardStatsResponse
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &statsResponse); err != nil {
		t.Fatalf("解析统计响应失败: %v", err)
	}
	if statsResponse.Base.MemoryTotal < 2 {
		t.Fatalf("记忆总数异常: %+v", statsResponse.Base)
	}
	if statsResponse.Base.MemoryType.Summary < 1 || statsResponse.Base.MemoryType.Error < 1 {
		t.Fatalf("记忆类型统计异常: %+v", statsResponse.Base.MemoryType)
	}
	if len(statsResponse.Base.HotTags) == 0 || statsResponse.Base.HotTags[0].Tag != "高频" {
		t.Fatalf("热门标签统计异常: %+v", statsResponse.Base.HotTags)
	}
	if statsResponse.CacheRefreshIntervalSecs != 5 {
		t.Fatalf("缓存刷新间隔异常: %d", statsResponse.CacheRefreshIntervalSecs)
	}

	cacheRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/stats/cache", nil)
	cacheRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	cacheRecorder := httptest.NewRecorder()
	router.ServeHTTP(cacheRecorder, cacheRequest)
	if cacheRecorder.Code != http.StatusOK {
		t.Fatalf("缓存统计接口失败: status=%d body=%s", cacheRecorder.Code, cacheRecorder.Body.String())
	}
	var cacheResponse api.DashboardCacheStatsResponse
	if err := json.Unmarshal(cacheRecorder.Body.Bytes(), &cacheResponse); err != nil {
		t.Fatalf("解析缓存统计响应失败: %v", err)
	}
	if !cacheResponse.Cache.Enabled {
		t.Fatalf("缓存统计应标识为启用: %+v", cacheResponse.Cache)
	}
}

// TestRouterNonAdminCannotUpdateMemory 验证非管理员无法编辑记忆，避免普通用户绕过前端入口修改数据。
func TestRouterNonAdminCannotUpdateMemory(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	member, err := userService.CreateUser(ctx, user.CreateInput{Username: "member", RealName: "普通成员", Password: "secret-3", IsAdmin: false})
	if err != nil {
		t.Fatalf("创建普通用户失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

	writeBody := bytes.NewReader([]byte(`{"project_name":"router-update","git_branch":"main","items":[{"type":"summary","title":"只读记忆","tags":["只读"],"summary":"不允许普通成员编辑","context":"原始正文"}]}`))
	writeRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/write", writeBody)
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRequest.Header.Set("X-API-Token", admin.APIToken)
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("写入记忆失败: status=%d body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}
	items, err := store.ListAllMemories()
	if err != nil || len(items) != 1 {
		t.Fatalf("读取记忆失败: err=%v items=%+v", err, items)
	}

	memberLoginBody := bytes.NewReader([]byte(`{"username":"member","password":"secret-3"}`))
	memberLoginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", memberLoginBody)
	memberLoginRequest.Header.Set("Content-Type", "application/json")
	memberLoginRecorder := httptest.NewRecorder()
	router.ServeHTTP(memberLoginRecorder, memberLoginRequest)
	if memberLoginRecorder.Code != http.StatusOK {
		t.Fatalf("普通用户登录失败: status=%d body=%s", memberLoginRecorder.Code, memberLoginRecorder.Body.String())
	}
	var memberLoginResponse api.LoginResponse
	if err := json.Unmarshal(memberLoginRecorder.Body.Bytes(), &memberLoginResponse); err != nil {
		t.Fatalf("解析普通用户登录响应失败: %v", err)
	}

	updateBody := bytes.NewReader([]byte(`{"title":"越权编辑","tags":["越权"],"summary":"越权总结","content":"越权正文"}`))
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/memories/"+strconv.FormatInt(items[0].ID, 10), updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Authorization", "Bearer "+memberLoginResponse.Token)
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusForbidden {
		t.Fatalf("普通用户编辑应被拒绝: status=%d body=%s user=%+v", updateRecorder.Code, updateRecorder.Body.String(), member)
	}

	stored, err := store.GetMemoryByID(items[0].ID)
	if err != nil {
		t.Fatalf("回读原始记忆失败: %v", err)
	}
	if stored.Title != "只读记忆" || stored.Content != "原始正文" {
		t.Fatalf("普通用户不应修改成功: %+v", stored)
	}
}

// TestRouterUpdateMemoryReturnsNotFound 验证编辑不存在的记忆时返回 404，避免前端误判为保存成功。
func TestRouterUpdateMemoryReturnsNotFound(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

	loginBody := bytes.NewReader([]byte(`{"username":"admin","password":"` + password + `"}`))
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("登录失败: status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse api.LoginResponse
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}

	updateBody := bytes.NewReader([]byte(`{"title":"不存在","tags":[],"summary":"不存在","content":"不存在"}`))
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/memories/99999", updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusNotFound {
		t.Fatalf("编辑不存在记忆应返回404: status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
}

// TestRouterAdminCanUpdateMemory 验证管理员可编辑记忆且返回最新详情，避免后台编辑后仍需额外刷新详情接口。
func TestRouterAdminCanUpdateMemory(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)

	writeBody := bytes.NewReader([]byte(`{"project_name":"router-update","git_branch":"main","items":[{"type":"summary","title":"编辑前标题","tags":["旧标签"],"summary":"编辑前总结","context":"编辑前正文"}]}`))
	writeRequest := httptest.NewRequest(http.MethodPost, "/tokenapi/v1/memories/write", writeBody)
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRequest.Header.Set("X-API-Token", admin.APIToken)
	writeRecorder := httptest.NewRecorder()
	router.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("写入记忆失败: status=%d body=%s", writeRecorder.Code, writeRecorder.Body.String())
	}
	items, err := store.ListAllMemories()
	if err != nil || len(items) != 1 {
		t.Fatalf("读取记忆失败: err=%v items=%+v", err, items)
	}
	memoryID := items[0].ID

	loginBody := bytes.NewReader([]byte(`{"username":"admin","password":"` + password + `"}`))
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("登录失败: status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse api.LoginResponse
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}

	updateBody := bytes.NewReader([]byte(`{"title":"编辑后标题","tags":["新标签","向量同步"],"summary":"编辑后总结","content":"编辑后正文"}`))
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/memories/"+strconv.FormatInt(memoryID, 10), updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("更新记忆失败: status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updateResponse api.MemoryDetailResponse
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updateResponse); err != nil {
		t.Fatalf("解析更新响应失败: %v", err)
	}
	if updateResponse.Item.Title != "编辑后标题" || updateResponse.Item.Summary != "编辑后总结" || updateResponse.Item.Content != "编辑后正文" {
		t.Fatalf("更新响应未返回最新详情: %+v", updateResponse)
	}
	if updateResponse.Item.ProjectName != "router-update" || updateResponse.Item.Type != "summary" {
		t.Fatalf("更新响应不应改动只读字段: %+v", updateResponse)
	}
}

// TestRouterNonAdminCannotMergeProjectMemories 验证非管理员无法执行项目合并，避免普通成员批量改写项目隔离维度。
func TestRouterNonAdminCannotMergeProjectMemories(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, _, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	if _, err := userService.CreateUser(ctx, user.CreateInput{Username: "member", RealName: "普通成员", Password: "secret-3", IsAdmin: false}); err != nil {
		t.Fatalf("创建普通用户失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	if _, err := service.Write(ctx, "main-project", "main", admin.UserID, []api.MemoryWriteItem{{Type: "summary", Title: "主项目", Tags: []string{"主"}, Summary: "主", Context: "主"}}); err != nil {
		t.Fatalf("写入主项目记忆失败: %v", err)
	}
	if _, err := service.Write(ctx, "alias-project", "main", admin.UserID, []api.MemoryWriteItem{{Type: "summary", Title: "副项目", Tags: []string{"副"}, Summary: "副", Context: "副"}}); err != nil {
		t.Fatalf("写入副项目记忆失败: %v", err)
	}

	memberLoginBody := bytes.NewReader([]byte(`{"username":"member","password":"secret-3"}`))
	memberLoginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", memberLoginBody)
	memberLoginRequest.Header.Set("Content-Type", "application/json")
	memberLoginRecorder := httptest.NewRecorder()
	router.ServeHTTP(memberLoginRecorder, memberLoginRequest)
	if memberLoginRecorder.Code != http.StatusOK {
		t.Fatalf("普通用户登录失败: status=%d body=%s", memberLoginRecorder.Code, memberLoginRecorder.Body.String())
	}
	var memberLoginResponse api.LoginResponse
	if err := json.Unmarshal(memberLoginRecorder.Body.Bytes(), &memberLoginResponse); err != nil {
		t.Fatalf("解析普通用户登录响应失败: %v", err)
	}

	mergeBody := bytes.NewReader([]byte(`{"source_project_name":"alias-project","target_project_name":"main-project"}`))
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/merge-project", mergeBody)
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeRequest.Header.Set("Authorization", "Bearer "+memberLoginResponse.Token)
	mergeRecorder := httptest.NewRecorder()
	router.ServeHTTP(mergeRecorder, mergeRequest)
	if mergeRecorder.Code != http.StatusForbidden {
		t.Fatalf("普通用户合并项目应被拒绝: status=%d body=%s", mergeRecorder.Code, mergeRecorder.Body.String())
	}
}

// TestRouterAdminCanListAndMergeProjectMemories 验证管理员可拉取项目下拉并执行项目合并。
func TestRouterAdminCanListAndMergeProjectMemories(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	if _, err := service.Write(ctx, "main-project", "main", admin.UserID, []api.MemoryWriteItem{{Type: "summary", Title: "主项目", Tags: []string{"主"}, Summary: "主", Context: "主"}}); err != nil {
		t.Fatalf("写入主项目记忆失败: %v", err)
	}
	if _, err := service.Write(ctx, "alias-project", "main", admin.UserID, []api.MemoryWriteItem{{Type: "summary", Title: "副项目", Tags: []string{"副"}, Summary: "副", Context: "副"}}); err != nil {
		t.Fatalf("写入副项目记忆失败: %v", err)
	}
	if err := store.ReplacePendingCleanupReviews(time.Now().UTC().Format(time.RFC3339Nano), "summary", []models.MemoryCleanupReview{{MemoryID: 1, ProjectName: "alias-project", Type: "summary", Status: "pending", Score: 0.8, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: time.Now().UTC().Format(time.RFC3339Nano), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}); err != nil {
		t.Fatalf("写入副项目审核记录失败: %v", err)
	}
	token := loginAsAdmin(t, router, password)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/memories/projects", nil)
	listRequest.Header.Set("Authorization", "Bearer "+token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("读取项目列表失败: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.ProjectNameListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析项目列表失败: %v", err)
	}
	if len(listResponse.Items) != 2 || listResponse.Items[0] != "alias-project" || listResponse.Items[1] != "main-project" {
		t.Fatalf("项目列表异常: %+v", listResponse)
	}

	mergeBody := bytes.NewReader([]byte(`{"source_project_name":"alias-project","target_project_name":"main-project"}`))
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/merge-project", mergeBody)
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeRequest.Header.Set("Authorization", "Bearer "+token)
	mergeRecorder := httptest.NewRecorder()
	router.ServeHTTP(mergeRecorder, mergeRequest)
	if mergeRecorder.Code != http.StatusOK {
		t.Fatalf("执行项目合并失败: status=%d body=%s", mergeRecorder.Code, mergeRecorder.Body.String())
	}
	var mergeResponse api.MergeProjectResponse
	if err := json.Unmarshal(mergeRecorder.Body.Bytes(), &mergeResponse); err != nil {
		t.Fatalf("解析项目合并响应失败: %v", err)
	}
	if mergeResponse.MergedMemoryCount != 1 || mergeResponse.TargetProjectName != "main-project" {
		t.Fatalf("项目合并响应异常: %+v", mergeResponse)
	}
	if mergeResponse.InvalidatedApprovedReviewCount != 0 {
		t.Fatalf("当前路由用例不应包含已批准失效记录: %+v", mergeResponse)
	}
	aliasItems, err := store.ListMemoriesByProjectAndType("alias-project", "summary")
	if err != nil {
		t.Fatalf("读取副项目记忆失败: %v", err)
	}
	if len(aliasItems) != 0 {
		t.Fatalf("副项目记忆应已被迁走: %+v", aliasItems)
	}
	mainItems, err := store.ListMemoriesByProjectAndType("main-project", "summary")
	if err != nil {
		t.Fatalf("读取主项目记忆失败: %v", err)
	}
	if len(mainItems) != 2 {
		t.Fatalf("主项目记忆数量异常: %d", len(mainItems))
	}
	reviews, total, err := store.ListCleanupReviews("", "summary", "alias-project", 1, 10)
	if err != nil {
		t.Fatalf("读取副项目审核记录失败: %v", err)
	}
	if total != 0 || len(reviews) != 0 {
		t.Fatalf("副项目审核记录应已清空: total=%d items=%+v", total, reviews)
	}
}

// TestRouterMergeProjectMemoriesRejectsEmptyProjectNames 验证空项目名不会被默认项目名吞掉，避免误合并到 default-project。
func TestRouterMergeProjectMemoriesRejectsEmptyProjectNames(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	if _, err := service.Write(ctx, "default-project", "main", admin.UserID, []api.MemoryWriteItem{{Type: "summary", Title: "默认项目", Tags: []string{"默认"}, Summary: "默认", Context: "默认"}}); err != nil {
		t.Fatalf("写入默认项目记忆失败: %v", err)
	}
	token := loginAsAdmin(t, router, password)

	mergeBody := bytes.NewReader([]byte(`{"source_project_name":"","target_project_name":"default-project"}`))
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/merge-project", mergeBody)
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeRequest.Header.Set("Authorization", "Bearer "+token)
	mergeRecorder := httptest.NewRecorder()
	router.ServeHTTP(mergeRecorder, mergeRequest)
	if mergeRecorder.Code != http.StatusBadRequest {
		t.Fatalf("空项目名应返回400: status=%d body=%s", mergeRecorder.Code, mergeRecorder.Body.String())
	}
	var mergeResponse api.MergeProjectResponse
	if err := json.Unmarshal(mergeRecorder.Body.Bytes(), &mergeResponse); err != nil {
		t.Fatalf("解析空项目名响应失败: %v", err)
	}
	if !strings.Contains(mergeResponse.Error, "不能为空") {
		t.Fatalf("空项目名错误信息异常: %+v", mergeResponse)
	}
	defaultItems, err := store.ListMemoriesByProjectAndType("default-project", "summary")
	if err != nil {
		t.Fatalf("读取默认项目记忆失败: %v", err)
	}
	if len(defaultItems) != 1 {
		t.Fatalf("空项目名请求不应改动默认项目数据: %+v", defaultItems)
	}
}

// TestRouterAdminCanListAndMergeProjectTags 验证管理员可读取项目标签并执行项目内标签合并。
func TestRouterAdminCanListAndMergeProjectTags(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	if _, err := service.Write(ctx, "tag-project", "main", admin.UserID, []api.MemoryWriteItem{
		{Type: "summary", Title: "记忆A", Tags: []string{"旧标签", "主标签"}, Summary: "A", Context: "A"},
		{Type: "summary", Title: "记忆B", Tags: []string{"待统一"}, Summary: "B", Context: "B"},
	}); err != nil {
		t.Fatalf("写入标签项目记忆失败: %v", err)
	}
	items, err := store.ListMemoriesByProjectAndType("tag-project", "summary")
	if err != nil || len(items) != 2 {
		t.Fatalf("读取标签项目记忆失败: err=%v items=%+v", err, items)
	}
	runAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(runAt, "summary", []models.MemoryCleanupReview{{MemoryID: items[0].ID, ProjectName: "tag-project", Type: "summary", Status: "pending", Score: 0.8, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: runAt, CreatedAt: runAt}}); err != nil {
		t.Fatalf("写入标签项目待审核记录失败: %v", err)
	}
	approvedAt := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(approvedAt, "summary", []models.MemoryCleanupReview{{MemoryID: items[1].ID, ProjectName: "tag-project", Type: "summary", Status: "approved", Score: 0.7, ReasonJSON: "{}", SnapshotJSON: "{}", RunAt: approvedAt, CreatedAt: approvedAt}}); err != nil {
		t.Fatalf("写入标签项目已批准记录失败: %v", err)
	}
	token := loginAsAdmin(t, router, password)

	tagListRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/memories/project-tags?project_name=tag-project", nil)
	tagListRequest.Header.Set("Authorization", "Bearer "+token)
	tagListRecorder := httptest.NewRecorder()
	router.ServeHTTP(tagListRecorder, tagListRequest)
	if tagListRecorder.Code != http.StatusOK {
		t.Fatalf("读取项目标签失败: status=%d body=%s", tagListRecorder.Code, tagListRecorder.Body.String())
	}
	var tagListResponse api.ProjectTagListResponse
	if err := json.Unmarshal(tagListRecorder.Body.Bytes(), &tagListResponse); err != nil {
		t.Fatalf("解析项目标签响应失败: %v", err)
	}
	if len(tagListResponse.Items) != 3 || tagListResponse.Items[0] != "主标签" || tagListResponse.Items[1] != "待统一" || tagListResponse.Items[2] != "旧标签" {
		t.Fatalf("项目标签列表异常: %+v", tagListResponse)
	}

	mergeBody := bytes.NewReader([]byte(`{"project_name":"tag-project","source_tags":["旧标签","待统一"],"target_tag":"主标签"}`))
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/merge-tags", mergeBody)
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeRequest.Header.Set("Authorization", "Bearer "+token)
	mergeRecorder := httptest.NewRecorder()
	router.ServeHTTP(mergeRecorder, mergeRequest)
	if mergeRecorder.Code != http.StatusOK {
		t.Fatalf("执行标签合并失败: status=%d body=%s", mergeRecorder.Code, mergeRecorder.Body.String())
	}
	var mergeResponse api.MergeTagsResponse
	if err := json.Unmarshal(mergeRecorder.Body.Bytes(), &mergeResponse); err != nil {
		t.Fatalf("解析标签合并响应失败: %v", err)
	}
	if mergeResponse.AffectedMemoryCount != 2 || mergeResponse.TargetTag != "主标签" {
		t.Fatalf("标签合并响应异常: %+v", mergeResponse)
	}
	updatedItems, err := store.ListMemoriesByProjectAndType("tag-project", "summary")
	if err != nil {
		t.Fatalf("读取合并后的项目记忆失败: %v", err)
	}
	for _, item := range updatedItems {
		tags := models.DecodeTags(item.Tags)
		if strings.Contains(item.Title, "记忆") {
			if countString(tags, "主标签") != 1 {
				t.Fatalf("主标签应已去重: %+v", tags)
			}
			if countString(tags, "旧标签") != 0 || countString(tags, "待统一") != 0 {
				t.Fatalf("副标签应已被移除: %+v", tags)
			}
		}
	}
	pendingReviews, pendingTotal, err := store.ListCleanupReviews("pending", "summary", "tag-project", 1, 10)
	if err != nil {
		t.Fatalf("读取标签项目待审核记录失败: %v", err)
	}
	if pendingTotal != 0 || len(pendingReviews) != 0 {
		t.Fatalf("待审核记录应已清理: total=%d items=%+v", pendingTotal, pendingReviews)
	}
	rejectedReviews, rejectedTotal, err := store.ListCleanupReviews("rejected", "summary", "tag-project", 1, 10)
	if err != nil {
		t.Fatalf("读取标签项目失效记录失败: %v", err)
	}
	if rejectedTotal != 1 || len(rejectedReviews) != 1 {
		t.Fatalf("已批准记录应被失效化: total=%d items=%+v", rejectedTotal, rejectedReviews)
	}
}

// TestRouterMergeProjectTagsRejectsNonAdmin 验证普通用户不能执行标签合并，避免越权修改项目记忆标签。
func TestRouterMergeProjectTagsRejectsNonAdmin(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	admin, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	if _, err := service.Write(ctx, "tag-project", "main", admin.UserID, []api.MemoryWriteItem{{Type: "summary", Title: "记忆A", Tags: []string{"旧标签"}, Summary: "A", Context: "A"}}); err != nil {
		t.Fatalf("写入标签项目记忆失败: %v", err)
	}
	memberCreateBody := bytes.NewReader([]byte(`{"username":"member","real_name":"普通成员","password":"member-pass","is_admin":false}`))
	memberCreateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", memberCreateBody)
	memberCreateRequest.Header.Set("Content-Type", "application/json")
	memberCreateRequest.Header.Set("Authorization", "Bearer "+loginAsAdmin(t, router, password))
	memberCreateRecorder := httptest.NewRecorder()
	router.ServeHTTP(memberCreateRecorder, memberCreateRequest)
	if memberCreateRecorder.Code != http.StatusOK {
		t.Fatalf("创建普通用户失败: status=%d body=%s", memberCreateRecorder.Code, memberCreateRecorder.Body.String())
	}
	memberLoginBody := bytes.NewReader([]byte(`{"username":"member","password":"member-pass"}`))
	memberLoginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", memberLoginBody)
	memberLoginRequest.Header.Set("Content-Type", "application/json")
	memberLoginRecorder := httptest.NewRecorder()
	router.ServeHTTP(memberLoginRecorder, memberLoginRequest)
	if memberLoginRecorder.Code != http.StatusOK {
		t.Fatalf("普通用户登录失败: status=%d body=%s", memberLoginRecorder.Code, memberLoginRecorder.Body.String())
	}
	var memberLoginResponse api.LoginResponse
	if err := json.Unmarshal(memberLoginRecorder.Body.Bytes(), &memberLoginResponse); err != nil {
		t.Fatalf("解析普通用户登录响应失败: %v", err)
	}
	mergeBody := bytes.NewReader([]byte(`{"project_name":"tag-project","source_tags":["旧标签"],"target_tag":"主标签"}`))
	mergeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/merge-tags", mergeBody)
	mergeRequest.Header.Set("Content-Type", "application/json")
	mergeRequest.Header.Set("Authorization", "Bearer "+memberLoginResponse.Token)
	mergeRecorder := httptest.NewRecorder()
	router.ServeHTTP(mergeRecorder, mergeRequest)
	if mergeRecorder.Code != http.StatusForbidden {
		t.Fatalf("普通用户合并标签应被拒绝: status=%d body=%s", mergeRecorder.Code, mergeRecorder.Body.String())
	}
}

// TestRouterProtectedTagCRUD 验证管理员可通过接口维护保护标签，避免白名单只能通过改配置文件管理。
func TestRouterProtectedTagCRUD(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	token := loginAsAdmin(t, router, password)

	createBody := bytes.NewReader([]byte(`{"tag":"核心故障","description":"长期保留","enabled":true}`))
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/protected-tags", createBody)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Authorization", "Bearer "+token)
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("创建保护标签失败: status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var createResponse api.ProtectedTagMutationResponse
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createResponse); err != nil {
		t.Fatalf("解析创建保护标签响应失败: %v", err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/memories/protected-tags", nil)
	listRequest.Header.Set("Authorization", "Bearer "+token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("读取保护标签失败: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.ProtectedTagListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析保护标签列表失败: %v", err)
	}
	if len(listResponse.Items) != 1 || listResponse.Items[0].Tag != "核心故障" {
		t.Fatalf("保护标签列表异常: %+v", listResponse)
	}

	updateBody := bytes.NewReader([]byte(`{"tag":"架构决策","description":"改名后仍启用","enabled":true}`))
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/memories/protected-tags/"+strconv.FormatInt(createResponse.Item.ID, 10), updateBody)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Authorization", "Bearer "+token)
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("更新保护标签失败: status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/memories/protected-tags/"+strconv.FormatInt(createResponse.Item.ID, 10), nil)
	deleteRequest.Header.Set("Authorization", "Bearer "+token)
	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("删除保护标签失败: status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

// TestRouterCleanupReviewsRunAndList 验证管理员可触发候选生成并查看审核列表，避免治理页缺少真实数据入口。
func TestRouterCleanupReviewsRunAndList(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret", ScheduleConfig: &config.ScheduleConfig{MemoryCleanup: config.MemoryCleanupScheduleConfig{
		Mode:       "review",
		ReviewTopN: 20,
		Summary:    config.MemoryCleanupPolicyConfig{BeforeDays: 1, BatchSize: 10, MinRemaining: 0, MinRemainingPerProject: 0, ScoreThreshold: 0, Weights: config.MemoryCleanupWeightsConfig{Age: 0.3, UseCount: 0.35, LastUsed: 0.25, ProjectPressure: 0.1}},
		Error:      config.MemoryCleanupPolicyConfig{BeforeDays: 1, BatchSize: 10, MinRemaining: 0, MinRemainingPerProject: 0, ScoreThreshold: 0, Weights: config.MemoryCleanupWeightsConfig{Age: 0.2, UseCount: 0.25, LastUsed: 0.35, ProjectPressure: 0.2}},
	}}})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	now := time.Now().UTC().AddDate(0, 0, -7)
	if _, err := store.CreateMemories([]models.Memory{{UserID: "admin", ProjectName: "cleanup-route", GitBranch: "main", Type: "summary", Title: "清理候选", Tags: models.EncodeTags([]string{"普通"}), Summary: "待清理", Content: "内容", Timestamp: now.Format("20060102150405"), CreatedAt: now.Format(time.RFC3339Nano)}}); err != nil {
		t.Fatalf("写入测试候选失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	token := loginAsAdmin(t, router, password)

	runRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/cleanup-reviews/run", nil)
	runRequest.Header.Set("Authorization", "Bearer "+token)
	runRecorder := httptest.NewRecorder()
	router.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("生成审核候选失败: status=%d body=%s", runRecorder.Code, runRecorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/memories/cleanup-reviews?page=1&page_size=10&type=summary", nil)
	listRequest.Header.Set("Authorization", "Bearer "+token)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("读取审核列表失败: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse api.CleanupReviewListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("解析审核列表失败: %v", err)
	}
	if len(listResponse.Items) != 1 || listResponse.Items[0].Snapshot["title"] != "清理候选" {
		t.Fatalf("审核列表内容异常: %+v", listResponse)
	}
}

// TestRouterCleanupExecuteOnlySelected 验证执行接口只会处理管理员勾选且已批准的审核记录。
func TestRouterCleanupExecuteOnlySelected(t *testing.T) {
	t.Helper()
	memoryRoot := t.TempDir()
	service := memory.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	userService := user.NewService(config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	ctx, store := testContextWithStore(t, config.AppConfig{MemoryRoot: memoryRoot, JWTSecret: "test-secret"})
	_, password, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		t.Fatalf("初始化默认管理员失败: %v", err)
	}
	now := time.Now().UTC().AddDate(0, 0, -7)
	items, err := store.CreateMemories([]models.Memory{
		{UserID: "admin", ProjectName: "cleanup-route", GitBranch: "main", Type: "summary", Title: "执行删除", Tags: models.EncodeTags([]string{"普通"}), Summary: "删除", Content: "内容", Timestamp: now.Format("20060102150405"), CreatedAt: now.Format(time.RFC3339Nano)},
		{UserID: "admin", ProjectName: "cleanup-route", GitBranch: "main", Type: "summary", Title: "保留记录", Tags: models.EncodeTags([]string{"普通"}), Summary: "保留", Content: "内容", Timestamp: now.Add(-time.Hour).Format("20060102150405"), CreatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano)},
	})
	if err != nil {
		t.Fatalf("写入测试记忆失败: %v", err)
	}
	runAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.ReplacePendingCleanupReviews(runAt, "summary", []models.MemoryCleanupReview{{MemoryID: items[0].ID, ProjectName: items[0].ProjectName, Type: "summary", Status: "pending", Score: 0.9, RunAt: runAt, CreatedAt: runAt}}); err != nil {
		t.Fatalf("写入审核记录A失败: %v", err)
	}
	if err := store.ReplacePendingCleanupReviews(runAt+"-b", "summary", []models.MemoryCleanupReview{{MemoryID: items[1].ID, ProjectName: items[1].ProjectName, Type: "summary", Status: "pending", Score: 0.8, RunAt: runAt + "-b", CreatedAt: runAt}}); err != nil {
		t.Fatalf("写入审核记录B失败: %v", err)
	}
	reviews, total, err := store.ListCleanupReviews("pending", "summary", "", 1, 10)
	if err != nil || total != 2 {
		t.Fatalf("读取审核记录失败: err=%v total=%d items=%+v", err, total, reviews)
	}
	selectedID := reviews[0].ID
	if reviews[0].MemoryID != items[0].ID {
		selectedID = reviews[1].ID
	}
	if err := service.ApproveCleanupReviews(ctx, []int64{selectedID}, "admin"); err != nil {
		t.Fatalf("批准审核记录失败: %v", err)
	}
	router := newTestRouter(t, service, userService, store)
	token := loginAsAdmin(t, router, password)

	executeBody := bytes.NewReader([]byte(`{"ids":[` + strconv.FormatInt(selectedID, 10) + `]}`))
	executeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/memories/cleanup-reviews/execute", executeBody)
	executeRequest.Header.Set("Content-Type", "application/json")
	executeRequest.Header.Set("Authorization", "Bearer "+token)
	executeRecorder := httptest.NewRecorder()
	router.ServeHTTP(executeRecorder, executeRequest)
	if executeRecorder.Code != http.StatusOK {
		t.Fatalf("执行审核记录失败: status=%d body=%s", executeRecorder.Code, executeRecorder.Body.String())
	}
	remaining, err := store.ListAllMemories()
	if err != nil {
		t.Fatalf("读取剩余记忆失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Title != "保留记录" {
		t.Fatalf("执行接口不应删除未勾选记录: %+v", remaining)
	}
}

func loginAsAdmin(t *testing.T, router *gin.Engine, password string) string {
	t.Helper()
	loginBody := bytes.NewReader([]byte(`{"username":"admin","password":"` + password + `"}`))
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("管理员登录失败: status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse api.LoginResponse
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("解析管理员登录响应失败: %v", err)
	}
	return loginResponse.Token
}

// countString 统计字符串切片中目标值出现次数，避免测试遗漏重复标签未清理的问题。
func countString(items []string, target string) int {
	count := 0
	for _, item := range items {
		if item == target {
			count++
		}
	}
	return count
}
