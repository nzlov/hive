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

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/memories?page=1&page_size=10&queries=管理列表关键字命中", nil)
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
	if listResponse.Items[0].Confidence == nil || *listResponse.Items[0].Confidence <= 0 {
		t.Fatalf("记忆列表搜索结果应返回置信度: %+v", listResponse.Items[0])
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

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/memories?page=1&page_size=10", nil)
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
