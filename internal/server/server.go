package server

import (
	"net/http"
	"time"

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
		result, err := service.Search(request.ProjectRoot, request.ProjectName, request.Queries, request.Debug)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, api.SearchResponse{
			Query:         result.Query,
			SearchRoot:    result.SearchRoot,
			DebugCommands: result.DebugCommands,
			ErrorHits:     buildSearchHits(result.ErrorHits),
			SummaryHits:   buildSearchHits(result.SummaryHits),
			Markdown:      result.Markdown(),
		})
	})
	router.POST("/api/v1/memories/write", func(c *gin.Context) {
		var request api.WriteRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		databasePath, err := service.Write(request.ProjectRoot, request.ProjectName, request.GitBranch, request.Items)
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
			Path:        hit.Path,
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
