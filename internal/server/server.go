package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"memory-manager/internal/api"
	"memory-manager/internal/memory"
)

// NewRouter 构造 HTTP 路由，把输入校验和业务调用边界固定在一处。
func NewRouter(service *memory.Service) *gin.Engine {
	router := gin.Default()
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.POST("/api/v1/memories/search", func(c *gin.Context) {
		var request api.SearchRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		markdown, err := service.Search(request.ProjectRoot, request.Queries, request.Debug)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.SearchResponse{Markdown: markdown})
	})
	router.POST("/api/v1/memories/write", func(c *gin.Context) {
		var request api.WriteRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		databasePath, err := service.Write(request.ProjectRoot, request.Items)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.WriteResponse{DatabasePath: databasePath})
	})
	router.POST("/api/v1/memories/rebuild-embeddings", func(c *gin.Context) {
		var request api.RebuildRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, err := service.RebuildEmbeddings(request.ProjectRoot, request.Force)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.RebuildResponse{Changed: result.Changed, Message: result.Message})
	})
	return router
}
