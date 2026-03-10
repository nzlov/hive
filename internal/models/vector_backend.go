package models

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// VectorSimilarity 描述一次向量查询返回的最小结果结构，避免业务层直接拼接方言 SQL。
type VectorSimilarity struct {
	MemoryID   int64   `gorm:"column:memory_id;comment:命中记忆主键"`
	Similarity float64 `gorm:"column:similarity;comment:相似度分数"`
}

// VectorBackend 抽象不同数据库的向量能力，确保业务层可用同一接口完成写入和查询。
type VectorBackend interface {
	EnsureReady() error
	SearchSimilar(projectName, memType string, queryVector []float64, limit int) ([]VectorSimilarity, error)
}

type noopVectorBackend struct{}

// EnsureReady 在未知后端下直接失败，避免服务错误落入不受控的隐式降级。
func (noopVectorBackend) EnsureReady() error {
	return fmt.Errorf("不支持的向量后端")
}

// SearchSimilar 未知后端不允许执行向量查询，避免返回伪造结果误导调用方。
func (noopVectorBackend) SearchSimilar(string, string, []float64, int) ([]VectorSimilarity, error) {
	return nil, fmt.Errorf("不支持的向量后端")
}

type sqliteVecBackend struct {
	// db 持有当前 SQLite 连接，便于直接执行 sqlite-vec 相关原生 SQL。
	db *gorm.DB
}

// EnsureReady 强校验 sqlite-vec 能力，避免服务启动后才暴露扩展缺失问题。
func (b sqliteVecBackend) EnsureReady() error {
	var version string
	if err := b.db.Raw("SELECT vec_version()").Scan(&version).Error; err != nil {
		return fmt.Errorf("sqlite-vec 扩展不可用: %w", err)
	}
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("sqlite-vec 扩展版本为空")
	}
	return nil
}

// SearchSimilar 通过 sqlite-vec 距离函数执行向量检索，避免业务层再做全量向量扫描。
func (b sqliteVecBackend) SearchSimilar(projectName, memType string, queryVector []float64, limit int) ([]VectorSimilarity, error) {
	if limit <= 0 {
		return []VectorSimilarity{}, nil
	}
	queryJSON := EncodeVector(queryVector)
	rows := make([]VectorSimilarity, 0, limit)
	baseSQL := strings.Join([]string{
		"SELECT memory_id,",
		"1 - vec_distance_cosine(vector, ?) AS similarity",
		"FROM memory_embeddings",
		"WHERE type = ?",
	}, " ")
	args := []any{queryJSON, strings.TrimSpace(memType)}
	if cleanedProjectName := strings.TrimSpace(projectName); cleanedProjectName != "" {
		baseSQL += " AND project_name = ?"
		args = append(args, cleanedProjectName)
	}
	baseSQL += " ORDER BY vec_distance_cosine(vector, ?) ASC, timestamp DESC, memory_id DESC LIMIT ?"
	args = append(args, queryJSON, limit)
	if err := b.db.Raw(baseSQL, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

type pgVectorBackend struct {
	// db 持有当前 PostgreSQL 连接，便于执行 pgvector 扩展查询与校验。
	db *gorm.DB
}

// EnsureReady 启动时自动创建并验证 pgvector 扩展，失败即中断避免运行时隐患。
func (b pgVectorBackend) EnsureReady() error {
	if err := b.db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return fmt.Errorf("创建 pgvector 扩展失败: %w", err)
	}
	var distance float64
	if err := b.db.Raw("SELECT '[1,0]'::vector <=> '[1,0]'::vector").Scan(&distance).Error; err != nil {
		return fmt.Errorf("pgvector 扩展校验失败: %w", err)
	}
	return nil
}

// SearchSimilar 通过 pgvector 操作符执行向量检索，避免上层继续手工扫描和打分。
func (b pgVectorBackend) SearchSimilar(projectName, memType string, queryVector []float64, limit int) ([]VectorSimilarity, error) {
	if limit <= 0 {
		return []VectorSimilarity{}, nil
	}
	queryLiteral := vectorLiteral(queryVector)
	rows := make([]VectorSimilarity, 0, limit)
	baseSQL := strings.Join([]string{
		"SELECT memory_id,",
		"1 - ((vector)::vector <=> (?::vector)) AS similarity",
		"FROM memory_embeddings",
		"WHERE type = ?",
	}, " ")
	args := []any{queryLiteral, strings.TrimSpace(memType)}
	if cleanedProjectName := strings.TrimSpace(projectName); cleanedProjectName != "" {
		baseSQL += " AND project_name = ?"
		args = append(args, cleanedProjectName)
	}
	baseSQL += " ORDER BY ((vector)::vector <=> (?::vector)) ASC, timestamp DESC, memory_id DESC LIMIT ?"
	args = append(args, queryLiteral, limit)
	if err := b.db.Raw(baseSQL, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// newVectorBackend 根据驱动选择向量实现，避免服务层继续维护数据库方言分支。
func newVectorBackend(db *gorm.DB, driver string) VectorBackend {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "sqlite":
		return sqliteVecBackend{db: db}
	case "postgres":
		return pgVectorBackend{db: db}
	default:
		return noopVectorBackend{}
	}
}

// vectorLiteral 统一构造 pgvector 字面量，避免查询路径散落浮点格式细节。
func vectorLiteral(vector []float64) string {
	parts := make([]string, 0, len(vector))
	for _, item := range vector {
		parts = append(parts, strconv.FormatFloat(item, 'f', -1, 64))
	}
	return "[" + strings.Join(parts, ",") + "]"
}
