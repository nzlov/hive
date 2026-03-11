package server

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/memory"
	"github.com/nzlov/hive/internal/models"
	"github.com/nzlov/hive/internal/user"
	webui "github.com/nzlov/hive/web"
)

const currentUserContextKey = "current_user"

// NewRouter 构造 HTTP 路由，把管理端鉴权、记忆接口鉴权和前端静态资源入口统一收敛到一处。
func NewRouter(memoryService *memory.Service, userService *user.Service, store *models.Store, statsService *dashboardStatsService) *gin.Engine {
	if statsService == nil {
		statsService = NewDashboardStatsService()
		if store != nil {
			_ = statsService.Bootstrap(store)
		}
	}
	router := gin.Default()
	router.Use(buildDBStoreMiddleware(store))
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	registerUserRoutes(router, memoryService, userService, statsService)
	registerTokenMemoryRoutes(router, memoryService, userService, statsService)
	router.NoRoute(buildSPAFallbackHandler())
	return router
}

// registerUserRoutes 把登录态、统计接口、普通后台接口和管理员接口分组注册，避免权限边界散落在单个路由上。
func registerUserRoutes(router *gin.Engine, memoryService *memory.Service, userService *user.Service, statsService *dashboardStatsService) {
	authGroup := router.Group("/api/v1/users")
	authGroup.POST("/auth/login", func(c *gin.Context) {
		var request api.LoginRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.LoginResponse{Error: err.Error()})
			return
		}
		currentUser, err := userService.AuthenticateLogin(c.Request.Context(), request.Username, request.Password)
		if err != nil {
			c.JSON(http.StatusUnauthorized, api.LoginResponse{Error: err.Error()})
			return
		}
		token, err := user.SignJWT(userServiceSecret(userService), currentUser)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.LoginResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.LoginResponse{Token: token, User: userToSummary(currentUser)})
	})

	protectedGroup := router.Group("/api/v1")
	protectedGroup.Use(buildJWTMiddleware(userService))
	sessionGroup := protectedGroup.Group("/users")
	memoryGroup := protectedGroup.Group("/memories")
	adminGroup := protectedGroup.Group("/admin")
	adminGroup.Use(buildAdminOnlyMiddleware())
	adminUserGroup := adminGroup.Group("/users")
	adminMemoryGroup := adminGroup.Group("/memories")

	sessionGroup.POST("/auth/logout", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	adminUserGroup.GET("", func(c *gin.Context) {
		var request api.UserListRequest
		if err := c.ShouldBindQuery(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.UserListResponse{Error: err.Error()})
			return
		}
		items, total, err := userService.ListUsers(c.Request.Context(), request.Page, request.PageSize, request.Keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserListResponse{Error: err.Error()})
			return
		}
		page := request.Page
		if page < 1 {
			page = 1
		}
		pageSize := request.PageSize
		if pageSize < 1 {
			pageSize = 10
		}
		response := api.UserListResponse{
			Items:     make([]api.UserSummary, 0, len(items)),
			Total:     total,
			Page:      page,
			PageSize:  pageSize,
			TotalPage: buildTotalPages(total, pageSize),
		}
		for _, item := range items {
			response.Items = append(response.Items, userToSummary(item))
		}
		c.JSON(http.StatusOK, response)
	})
	adminUserGroup.POST("", func(c *gin.Context) {
		var request api.CreateUserRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: err.Error()})
			return
		}
		item, err := userService.CreateUser(c.Request.Context(), user.CreateInput{
			Username: request.Username,
			RealName: request.RealName,
			Password: request.Password,
			IsAdmin:  request.IsAdmin,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: err.Error()})
			return
		}
		statsService.OnUserCreated()
		c.JSON(http.StatusOK, api.UserMutationResponse{Item: userToSummary(item)})
	})
	adminUserGroup.PUT("/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: "无效的用户ID"})
			return
		}
		var request api.UpdateUserRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: err.Error()})
			return
		}
		item, err := userService.UpdateUser(c.Request.Context(), id, user.UpdateInput{
			Username:        request.Username,
			RealName:        request.RealName,
			Password:        request.Password,
			IsAdmin:         request.IsAdmin,
			RegenerateToken: request.RegenerateToken,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.UserMutationResponse{Item: userToSummary(item)})
	})
	adminUserGroup.DELETE("/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: "无效的用户ID"})
			return
		}
		currentUser := mustCurrentUser(c)
		if err := userService.DeleteUser(c.Request.Context(), id, currentUser.UserID); err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: err.Error()})
			return
		}
		statsService.OnUserDeleted()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	sessionGroup.GET("/stats", func(c *gin.Context) {
		cacheStats := memoryService.CacheStatsSnapshot()
		response := api.DashboardStatsResponse{
			Base: statsService.Snapshot(),
			Cache: api.DashboardCacheStats{
				Enabled:                  cacheStats.Enabled,
				QueryEmbeddingEntryCount: cacheStats.QueryEmbeddingEntryCount,
				SemanticHitsEntryCount:   cacheStats.SemanticHitsEntryCount,
				HitRate:                  cacheStats.HitRate,
				EstimatedMemoryBytes:     cacheStats.EstimatedMemoryBytes,
				EstimatedMemoryHuman:     formatBytesHuman(cacheStats.EstimatedMemoryBytes),
				QueryEmbeddingHitCount:   cacheStats.QueryEmbeddingHitCount,
				QueryEmbeddingMissCount:  cacheStats.QueryEmbeddingMissCount,
				SemanticHitsHitCount:     cacheStats.SemanticHitsHitCount,
				SemanticHitsMissCount:    cacheStats.SemanticHitsMissCount,
				QueryEmbeddingEvictCount: cacheStats.QueryEmbeddingEvictCount,
				SemanticHitsEvictCount:   cacheStats.SemanticHitsEvictCount,
			},
			CacheRefreshIntervalSecs: memoryService.CacheStatsRefreshInterval(),
		}
		c.JSON(http.StatusOK, response)
	})
	sessionGroup.GET("/stats/cache", func(c *gin.Context) {
		cacheStats := memoryService.CacheStatsSnapshot()
		response := api.DashboardCacheStatsResponse{Cache: api.DashboardCacheStats{
			Enabled:                  cacheStats.Enabled,
			QueryEmbeddingEntryCount: cacheStats.QueryEmbeddingEntryCount,
			SemanticHitsEntryCount:   cacheStats.SemanticHitsEntryCount,
			HitRate:                  cacheStats.HitRate,
			EstimatedMemoryBytes:     cacheStats.EstimatedMemoryBytes,
			EstimatedMemoryHuman:     formatBytesHuman(cacheStats.EstimatedMemoryBytes),
			QueryEmbeddingHitCount:   cacheStats.QueryEmbeddingHitCount,
			QueryEmbeddingMissCount:  cacheStats.QueryEmbeddingMissCount,
			SemanticHitsHitCount:     cacheStats.SemanticHitsHitCount,
			SemanticHitsMissCount:    cacheStats.SemanticHitsMissCount,
			QueryEmbeddingEvictCount: cacheStats.QueryEmbeddingEvictCount,
			SemanticHitsEvictCount:   cacheStats.SemanticHitsEvictCount,
		}}
		c.JSON(http.StatusOK, response)
	})
	sessionGroup.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, api.UserMutationResponse{Item: userToSummary(mustCurrentUser(c))})
	})
	sessionGroup.PUT("/me/password", func(c *gin.Context) {
		currentUser := mustCurrentUser(c)
		var request api.ChangePasswordRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: err.Error()})
			return
		}
		if strings.TrimSpace(request.NewPassword) == "" {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: "新密码不能为空"})
			return
		}
		store, err := models.StoreFromContext(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserMutationResponse{Error: err.Error()})
			return
		}
		existing, err := store.FindUserByID(currentUser.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserMutationResponse{Error: err.Error()})
			return
		}
		salt, err := randomString(8)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserMutationResponse{Error: err.Error()})
			return
		}
		passwordHash := hashPasswordBySalt(request.NewPassword, salt)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		updated, err := store.SaveUser(models.User{
			ID:           existing.ID,
			UserID:       existing.UserID,
			Username:     existing.Username,
			RealName:     existing.RealName,
			PasswordHash: passwordHash,
			Salt:         salt,
			APIToken:     existing.APIToken,
			IsAdmin:      existing.IsAdmin,
			CreatedAt:    existing.CreatedAt,
			UpdatedAt:    now,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserMutationResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.UserMutationResponse{Item: userToSummary(toUser(updated))})
	})

	memoryGroup.GET("", func(c *gin.Context) {
		var request api.MemoryListRequest
		if err := c.ShouldBindQuery(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.MemoryListResponse{Error: err.Error()})
			return
		}
		if strings.TrimSpace(request.Description) == "" {
			c.JSON(http.StatusBadRequest, api.MemoryListResponse{Error: "description 不能为空，请按 tags + description 传参"})
			return
		}
		result, err := memoryService.List(c.Request.Context(), request.Page, request.PageSize, request.Tags, request.Description)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryListResponse{Error: err.Error()})
			return
		}
		store, err := models.StoreFromContext(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryListResponse{Error: err.Error()})
			return
		}
		creatorNameMap, err := buildCreatorNameMap(store, collectMemoryUserIDs(result.Items))
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryListResponse{Error: err.Error()})
			return
		}
		response := api.MemoryListResponse{
			Items:     make([]api.MemoryItem, 0, len(result.Items)),
			Total:     result.Total,
			Page:      result.Page,
			PageSize:  result.PageSize,
			TotalPage: result.TotalPage,
		}
		for _, item := range result.Items {
			response.Items = append(response.Items, memoryToItem(item.Memory, creatorNameMap[item.Memory.UserID], item.Confidence))
		}
		c.JSON(http.StatusOK, response)
	})
	memoryGroup.GET("/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.MemoryDetailResponse{Error: "无效的记忆ID"})
			return
		}
		store, err := models.StoreFromContext(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		item, err := store.GetMemoryByID(id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, models.ErrNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		creatorNameMap, err := buildCreatorNameMap(store, []string{item.UserID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.MemoryDetailResponse{Item: memoryToDetail(item, creatorNameMap[item.UserID])})
	})
	adminMemoryGroup.PUT("/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.MemoryDetailResponse{Error: "无效的记忆ID"})
			return
		}
		var request api.UpdateMemoryRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		item, err := memoryService.Update(c.Request.Context(), id, request)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, models.ErrNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		store, err := models.StoreFromContext(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		creatorNameMap, err := buildCreatorNameMap(store, []string{item.UserID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.MemoryDetailResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.MemoryDetailResponse{Item: memoryToDetail(item, creatorNameMap[item.UserID])})
	})
	adminMemoryGroup.DELETE("/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.UserMutationResponse{Error: "无效的记忆ID"})
			return
		}
		store, err := models.StoreFromContext(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserMutationResponse{Error: err.Error()})
			return
		}
		var deletedMemory models.Memory
		if err := store.WithTx(func(txStore *models.Store) error {
			item, err := txStore.GetMemoryByID(id)
			if err != nil {
				return err
			}
			deletedMemory = item
			if err := txStore.DeleteMemoryEmbeddingByMemoryID(id); err != nil {
				return err
			}
			return txStore.DeleteMemoryByID(id)
		}); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, models.ErrNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, api.UserMutationResponse{Error: err.Error()})
			return
		}
		statsService.OnMemoryDeleted(deletedMemory)
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	adminMemoryGroup.GET("/projects", func(c *gin.Context) {
		items, err := memoryService.ListProjectNames(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.ProjectNameListResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.ProjectNameListResponse{Items: items})
	})
	adminMemoryGroup.GET("/project-tags", func(c *gin.Context) {
		projectName := strings.TrimSpace(c.Query("project_name"))
		items, err := memoryService.ListProjectTags(c.Request.Context(), projectName)
		if err != nil {
			status := http.StatusInternalServerError
			if memory.IsInvalidTagMergeInput(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, api.ProjectTagListResponse{ProjectName: projectName, Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.ProjectTagListResponse{ProjectName: projectName, Items: items})
	})
	adminMemoryGroup.POST("/merge-project", func(c *gin.Context) {
		var request api.MergeProjectRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.MergeProjectResponse{Error: err.Error()})
			return
		}
		result, err := memoryService.MergeProjectMemories(c.Request.Context(), request.SourceProjectName, request.TargetProjectName)
		if err != nil {
			status := http.StatusInternalServerError
			if memory.IsInvalidProjectMergeInput(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, api.MergeProjectResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.MergeProjectResponse{
			SourceProjectName:              result.SourceProjectName,
			TargetProjectName:              result.TargetProjectName,
			BatchCount:                     result.BatchCount,
			MergedMemoryCount:              result.MergedMemoryCount,
			RebuiltEmbeddingCount:          result.RebuiltEmbeddingCount,
			ClearedReviewCount:             result.ClearedReviewCount,
			InvalidatedApprovedReviewCount: result.InvalidatedApprovedReviewCount,
			Message:                        result.Message,
		})
	})
	adminMemoryGroup.POST("/merge-tags", func(c *gin.Context) {
		var request api.MergeTagsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.MergeTagsResponse{Error: err.Error()})
			return
		}
		result, err := memoryService.MergeMemoryTags(c.Request.Context(), request.ProjectName, request.SourceTags, request.TargetTag)
		if err != nil {
			status := http.StatusInternalServerError
			if memory.IsInvalidTagMergeInput(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, api.MergeTagsResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.MergeTagsResponse{
			ProjectName:                    result.ProjectName,
			SourceTags:                     result.SourceTags,
			TargetTag:                      result.TargetTag,
			BatchCount:                     result.BatchCount,
			AffectedMemoryCount:            result.AffectedMemoryCount,
			RebuiltEmbeddingCount:          result.RebuiltEmbeddingCount,
			ClearedPendingReviewCount:      result.ClearedPendingReviewCount,
			InvalidatedApprovedReviewCount: result.InvalidatedApprovedReviewCount,
			Message:                        result.Message,
		})
	})
	adminMemoryGroup.GET("/protected-tags", func(c *gin.Context) {
		items, err := memoryService.ListProtectedTags(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.ProtectedTagListResponse{Error: err.Error()})
			return
		}
		response := api.ProtectedTagListResponse{Items: make([]api.ProtectedTagItem, 0, len(items))}
		for _, item := range items {
			response.Items = append(response.Items, protectedTagToItem(item))
		}
		c.JSON(http.StatusOK, response)
	})
	adminMemoryGroup.POST("/protected-tags", func(c *gin.Context) {
		var request api.ProtectedTagMutationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.ProtectedTagMutationResponse{Error: err.Error()})
			return
		}
		item, err := memoryService.CreateProtectedTag(c.Request.Context(), request.Tag, request.Description, request.Enabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.ProtectedTagMutationResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.ProtectedTagMutationResponse{Item: protectedTagToItem(item)})
	})
	adminMemoryGroup.PUT("/protected-tags/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.ProtectedTagMutationResponse{Error: "无效的标签ID"})
			return
		}
		var request api.ProtectedTagMutationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.ProtectedTagMutationResponse{Error: err.Error()})
			return
		}
		item, err := memoryService.UpdateProtectedTag(c.Request.Context(), id, request.Tag, request.Description, request.Enabled)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, models.ErrNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, api.ProtectedTagMutationResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.ProtectedTagMutationResponse{Item: protectedTagToItem(item)})
	})
	adminMemoryGroup.DELETE("/protected-tags/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.ProtectedTagMutationResponse{Error: "无效的标签ID"})
			return
		}
		if err := memoryService.DeleteProtectedTag(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, api.ProtectedTagMutationResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	adminMemoryGroup.GET("/cleanup-reviews", func(c *gin.Context) {
		var request api.CleanupReviewListRequest
		if err := c.ShouldBindQuery(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.CleanupReviewListResponse{Error: err.Error()})
			return
		}
		items, total, err := memoryService.ListCleanupReviews(c.Request.Context(), request.Status, request.Type, request.ProjectName, request.Page, request.PageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.CleanupReviewListResponse{Error: err.Error()})
			return
		}
		page := request.Page
		if page < 1 {
			page = 1
		}
		pageSize := request.PageSize
		if pageSize < 1 {
			pageSize = 10
		}
		response := api.CleanupReviewListResponse{Items: make([]api.CleanupReviewItem, 0, len(items)), Total: total, Page: page, PageSize: pageSize, TotalPage: buildTotalPages(total, pageSize)}
		for _, item := range items {
			response.Items = append(response.Items, cleanupReviewToItem(item))
		}
		c.JSON(http.StatusOK, response)
	})
	adminMemoryGroup.POST("/cleanup-reviews/run", func(c *gin.Context) {
		result, err := memoryService.RunMemoryCleanupOnce(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.CleanupReviewRunResponse{Message: result.Message})
	})
	adminMemoryGroup.POST("/cleanup-reviews/approve", func(c *gin.Context) {
		var request api.CleanupReviewActionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		if err := memoryService.ApproveCleanupReviews(c.Request.Context(), request.IDs, mustCurrentUser(c).UserID); err != nil {
			c.JSON(http.StatusInternalServerError, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.CleanupReviewRunResponse{Message: "已批准所选候选。"})
	})
	adminMemoryGroup.POST("/cleanup-reviews/reject", func(c *gin.Context) {
		var request api.CleanupReviewActionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		if err := memoryService.RejectCleanupReviews(c.Request.Context(), request.IDs, mustCurrentUser(c).UserID); err != nil {
			c.JSON(http.StatusInternalServerError, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.CleanupReviewRunResponse{Message: "已拒绝所选候选。"})
	})
	adminMemoryGroup.POST("/cleanup-reviews/execute", func(c *gin.Context) {
		var request api.CleanupReviewActionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		if len(request.IDs) == 0 {
			c.JSON(http.StatusBadRequest, api.CleanupReviewRunResponse{Error: "请选择要执行的审核记录"})
			return
		}
		limit := len(request.IDs)
		result, err := memoryService.ExecuteApprovedCleanupReviews(c.Request.Context(), request.IDs, limit, mustCurrentUser(c).UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.CleanupReviewRunResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.CleanupReviewRunResponse{Message: result.Message})
	})
}

// buildAdminOnlyMiddleware 统一拦截非管理员访问敏感接口，避免在每个处理器中重复权限分支。
func buildAdminOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if mustCurrentUser(c).IsAdmin {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "仅管理员可访问"})
	}
}

// registerTokenMemoryRoutes 把记忆查询与写入统一挂到 API Token 路由下，避免和管理接口的 JWT 语义混淆。
func registerTokenMemoryRoutes(router *gin.Engine, memoryService *memory.Service, userService *user.Service, statsService *dashboardStatsService) {
	group := router.Group("/tokenapi/v1")
	group.Use(buildAPITokenMiddleware(userService))
	group.POST("/memories/search", func(c *gin.Context) {
		var request api.SearchRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.SearchResponse{Error: err.Error()})
			return
		}
		result, err := memoryService.Search(c.Request.Context(), request.ProjectName, request.Tags, request.Description, request.Debug)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.SearchResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.SearchResponse{
			Query:         result.Query,
			ProjectName:   result.ProjectName,
			DebugCommands: result.DebugCommands,
			ErrorHits:     buildSearchHits(result.ErrorHits),
			SummaryHits:   buildSearchHits(result.SummaryHits),
			Markdown:      result.Markdown(),
		})
	})
	group.POST("/memories/write", func(c *gin.Context) {
		var request api.WriteRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.WriteResponse{Error: err.Error()})
			return
		}
		currentUser := mustCurrentUser(c)
		_, err := memoryService.Write(c.Request.Context(), request.ProjectName, request.GitBranch, currentUser.UserID, request.Items)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.WriteResponse{Error: err.Error()})
			return
		}
		statsService.OnMemoryWritten(request.Items)
		c.JSON(http.StatusOK, api.WriteResponse{})
	})
}

// buildDBStoreMiddleware 把共享数据库连接注入请求上下文，确保服务层通过 context 统一取用。
func buildDBStoreMiddleware(store *models.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(models.StoreToContext(c.Request.Context(), store))
		c.Next()
	}
}

// buildJWTMiddleware 统一校验 Bearer JWT，并把当前用户注入上下文避免控制器重复解析令牌。
func buildJWTMiddleware(userService *user.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 Bearer Token"})
			return
		}
		rawToken := strings.TrimSpace(authorization[len("Bearer "):])
		claims, err := user.ParseJWT(userServiceSecret(userService), rawToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		currentUser, err := userService.FindByUserID(c.Request.Context(), claims.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set(currentUserContextKey, currentUser)
		c.Next()
	}
}

// buildAPITokenMiddleware 统一校验记忆接口的 API Token，并把调用用户传给后续写入逻辑。
func buildAPITokenMiddleware(userService *user.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiToken := strings.TrimSpace(c.GetHeader("X-API-Token"))
		if apiToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 X-API-Token"})
			return
		}
		currentUser, err := userService.AuthenticateAPIToken(c.Request.Context(), apiToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set(currentUserContextKey, currentUser)
		c.Next()
	}
}

// buildSPAFallbackHandler 在静态文件不存在时回退到 index.html，确保 Vue 单页路由刷新后仍可访问。
func buildSPAFallbackHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		if strings.HasPrefix(requestPath, "/api/") || strings.HasPrefix(requestPath, "/tokenapi/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		assetPath := cleanAssetPath(requestPath)
		payload, contentType, err := webui.ReadAsset(assetPath)
		if err == nil {
			c.Data(http.StatusOK, contentType, payload)
			return
		}
		payload, contentType, err = webui.ReadAsset("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "frontend asset missing")
			return
		}
		c.Data(http.StatusOK, contentType, payload)
	}
}

// buildSearchHits 把服务层命中结果转成稳定的 HTTP 出参，避免脚本依赖内部结构体。
func buildSearchHits(hits []memory.Hit) []api.SearchHit {
	result := make([]api.SearchHit, 0, len(hits))
	for _, hit := range hits {
		snippets := make([]api.SearchSnippet, 0, len(hit.Snippets))
		for _, snippet := range hit.Snippets {
			snippets = append(snippets, api.SearchSnippet{Start: snippet.Start, End: snippet.End, Content: snippet.Content})
		}
		result = append(result, api.SearchHit{
			Source:      hit.Source,
			GitBranch:   hit.GitBranch,
			Title:       hit.Title,
			Tags:        hit.Tags,
			Timestamp:   hit.Timestamp.Format(time.RFC3339),
			Confidence:  hit.Confidence,
			Snippets:    snippets,
			FileContent: hit.FileContent,
		})
	}
	return result
}

// mustCurrentUser 从上下文中读取已认证用户，保证受保护路由共享同一身份来源。
func mustCurrentUser(c *gin.Context) user.User {
	value, _ := c.Get(currentUserContextKey)
	currentUser, _ := value.(user.User)
	return currentUser
}

// userToSummary 统一裁剪对外返回字段，避免密码哈希等敏感数据泄漏给前端。
func userToSummary(item user.User) api.UserSummary {
	return api.UserSummary{
		ID:       item.ID,
		UserID:   item.UserID,
		Username: item.Username,
		RealName: item.RealName,
		APIToken: item.APIToken,
		IsAdmin:  item.IsAdmin,
	}
}

// memoryToItem 统一裁剪列表字段，并补齐创建人、使用次数和置信度以减少前端额外请求与解释成本。
func memoryToItem(item models.Memory, creatorName string, confidence *float64) api.MemoryItem {
	return api.MemoryItem{
		ID:          item.ID,
		ProjectName: item.ProjectName,
		Type:        item.Type,
		Title:       item.Title,
		Tags:        models.DecodeTags(item.Tags),
		Summary:     item.Summary,
		Confidence:  confidence,
		UserID:      item.UserID,
		CreatorName: strings.TrimSpace(creatorName),
		UseCount:    item.UseCount,
		CreatedAt:   item.CreatedAt,
	}
}

// memoryToDetail 统一构造详情视图数据，保证抽屉展示直接拿到后端补齐的创建人真实姓名。
func memoryToDetail(item models.Memory, creatorName string) api.MemoryDetail {
	return api.MemoryDetail{
		ID:          item.ID,
		ProjectName: item.ProjectName,
		GitBranch:   item.GitBranch,
		Type:        item.Type,
		Title:       item.Title,
		Tags:        models.DecodeTags(item.Tags),
		Summary:     item.Summary,
		Content:     item.Content,
		UserID:      item.UserID,
		CreatorName: strings.TrimSpace(creatorName),
		Timestamp:   item.Timestamp,
		CreatedAt:   item.CreatedAt,
		UseCount:    item.UseCount,
		LastUsedAt:  item.LastUsedAt,
	}
}

// protectedTagToItem 统一转换保护标签模型，避免前端直接依赖数据库字段命名。
func protectedTagToItem(item models.MemoryProtectedTag) api.ProtectedTagItem {
	return api.ProtectedTagItem{
		ID:          item.ID,
		Tag:         item.Tag,
		Enabled:     item.Enabled,
		Description: item.Description,
		Source:      item.Source,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

// cleanupReviewToItem 统一解析审核记录中的 JSON 快照，保证管理页能直接展示评分理由和候选摘要。
func cleanupReviewToItem(item models.MemoryCleanupReview) api.CleanupReviewItem {
	reason := map[string]any{}
	snapshot := map[string]any{}
	_ = json.Unmarshal([]byte(item.ReasonJSON), &reason)
	_ = json.Unmarshal([]byte(item.SnapshotJSON), &snapshot)
	return api.CleanupReviewItem{
		ID:            item.ID,
		MemoryID:      item.MemoryID,
		ProjectName:   item.ProjectName,
		Type:          item.Type,
		Status:        item.Status,
		Score:         item.Score,
		Reason:        reason,
		Snapshot:      snapshot,
		RunAt:         item.RunAt,
		ReviewedBy:    item.ReviewedBy,
		ReviewedAt:    item.ReviewedAt,
		ExecutionNote: item.ExecutionNote,
		CreatedAt:     item.CreatedAt,
	}
}

// collectMemoryUserIDs 收敛记忆创建人 ID，方便后端一次性补齐真实姓名避免前端自行查表。
func collectMemoryUserIDs(items []memory.MemoryListItem) []string {
	userIDs := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(item.Memory.UserID); value != "" {
			userIDs = append(userIDs, value)
		}
	}
	return userIDs
}

// buildCreatorNameMap 批量构建 userid 到真实姓名的映射，避免记忆列表和详情各自实现用户查询逻辑。
func buildCreatorNameMap(store *models.Store, userIDs []string) (map[string]string, error) {
	items, err := store.FindUsersByUserIDs(userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		userID := strings.TrimSpace(item.UserID)
		if userID == "" {
			continue
		}
		name := strings.TrimSpace(item.RealName)
		if name == "" {
			name = strings.TrimSpace(item.Username)
		}
		if name != "" {
			result[userID] = name
		}
	}
	return result, nil
}

// buildTotalPages 统一分页页数计算，避免前后端分别实现导致边界行为不一致。
func buildTotalPages(total int64, pageSize int) int {
	if pageSize <= 0 {
		pageSize = 10
	}
	if total == 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}

// userServiceSecret 统一读取 JWT 密钥，避免服务端各层自己决定签名配置来源。
func userServiceSecret(userService *user.Service) string {
	return userService.JWTSecret()
}

// cleanAssetPath 统一规范静态资源路径，避免路径穿越和重复斜杠导致读取异常。
func cleanAssetPath(requestPath string) string {
	cleaned := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if cleaned == "" || cleaned == "." {
		return "index.html"
	}
	return cleaned
}

// contentTypeByPath 根据文件后缀返回内容类型，避免静态资源回传时缺少浏览器可识别的响应头。
func contentTypeByPath(assetPath string) string {
	contentType := mime.TypeByExtension(filepath.Ext(assetPath))
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

// hashPasswordBySalt 使用 sha1(password+salt) 对密码进行哈希，与服务层保持一致的加密方式。
func hashPasswordBySalt(password, salt string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(password) + strings.TrimSpace(salt)))
	return hex.EncodeToString(sum[:])
}

// randomString 统一生成随机字符串，用于生成密码盐值等场景。
func randomString(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// toUser 将数据库用户模型转换为业务层用户对象，避免控制器直接依赖持久化结构。
func toUser(item models.User) user.User {
	return user.User{
		ID:           item.ID,
		UserID:       item.UserID,
		Username:     item.Username,
		RealName:     item.RealName,
		PasswordHash: item.PasswordHash,
		Salt:         item.Salt,
		APIToken:     item.APIToken,
		IsAdmin:      item.IsAdmin,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}
