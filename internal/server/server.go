package server

import (
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
func NewRouter(memoryService *memory.Service, userService *user.Service, store *models.Store) *gin.Engine {
	router := gin.Default()
	router.Use(buildDBStoreMiddleware(store))
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	registerUserRoutes(router, userService)
	registerTokenMemoryRoutes(router, memoryService, userService)
	router.NoRoute(buildSPAFallbackHandler())
	return router
}

// registerUserRoutes 把管理端登录和用户管理接口集中注册，避免 JWT 路由散落在多个文件中。
func registerUserRoutes(router *gin.Engine, userService *user.Service) {
	publicGroup := router.Group("/api/v1/users")
	publicGroup.POST("/auth/login", func(c *gin.Context) {
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

	protectedGroup := router.Group("/api/v1/users")
	protectedGroup.Use(buildJWTMiddleware(userService))
	protectedGroup.POST("/auth/logout", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	protectedGroup.GET("", func(c *gin.Context) {
		items, err := userService.ListUsers(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, api.UserListResponse{Error: err.Error()})
			return
		}
		response := api.UserListResponse{Items: make([]api.UserSummary, 0, len(items))}
		for _, item := range items {
			response.Items = append(response.Items, userToSummary(item))
		}
		c.JSON(http.StatusOK, response)
	})
	protectedGroup.POST("", func(c *gin.Context) {
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
		c.JSON(http.StatusOK, api.UserMutationResponse{Item: userToSummary(item)})
	})
	protectedGroup.PUT("/:id", func(c *gin.Context) {
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
	protectedGroup.DELETE("/:id", func(c *gin.Context) {
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
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	protectedGroup.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, api.UserMutationResponse{Item: userToSummary(mustCurrentUser(c))})
	})
}

// registerTokenMemoryRoutes 把记忆查询与写入统一挂到 API Token 路由下，避免和管理接口的 JWT 语义混淆。
func registerTokenMemoryRoutes(router *gin.Engine, memoryService *memory.Service, userService *user.Service) {
	group := router.Group("/tokenapi/v1")
	group.Use(buildAPITokenMiddleware(userService))
	group.POST("/memories/search", func(c *gin.Context) {
		var request api.SearchRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, api.SearchResponse{Error: err.Error()})
			return
		}
		result, err := memoryService.Search(c.Request.Context(), request.ProjectName, request.Queries, request.Debug)
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
			ProjectName: hit.ProjectName,
			GitBranch:   hit.GitBranch,
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
