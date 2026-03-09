package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// MemoryRow 收敛数据库记录字段，避免查询层直接暴露 SQL 细节。
type MemoryRow struct {
	ID          int64
	ProjectName string
	Type        string
	Title       string
	Tags        string
	Summary     string
	Content     string
	Timestamp   string
	CreatedAt   string
}

// getMemoryDBPath 统一数据库路径约定，避免不同子命令路径拼接不一致。
func getMemoryDBPath(memoryRoot string) string {
	return filepath.Join(memoryRoot, "memory.db")
}

// connectMemoryDB 连接并初始化数据库，确保首次运行也能直接读写。
func connectMemoryDB(memoryRoot string) (*sql.DB, error) {
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", getMemoryDBPath(memoryRoot))
	if err != nil {
		return nil, err
	}
	if err := initMemoryDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// initMemoryDB 初始化表结构与索引，保证检索排序和向量表状态稳定。
func initMemoryDB(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL CHECK(type IN ('summary', 'error')),
			title TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '[]',
			summary TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_memories_type_timestamp ON memories(type, timestamp DESC, id DESC);`,
		`CREATE TABLE IF NOT EXISTS memory_embeddings (
			memory_id INTEGER PRIMARY KEY,
			vector TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(memory_id) REFERENCES memories(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS memory_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_memories_project_type_timestamp ON memories(project_name, type, timestamp DESC, id DESC);`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return ensureProjectNameColumn(db)
}

// ensureProjectNameColumn 兼容旧库结构，避免升级后因缺列中断读写。
func ensureProjectNameColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(memories)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hasProjectName := false
	for rows.Next() {
		var cid int
		var name string
		var fieldType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &fieldType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "project_name" {
			hasProjectName = true
			break
		}
	}
	if hasProjectName {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE memories ADD COLUMN project_name TEXT NOT NULL DEFAULT ''`)
	return err
}

// encodeTags 使用 JSON 存储标签，避免分隔符污染内容本身。
func encodeTags(tags []string) string {
	content, _ := json.Marshal(tags)
	return string(content)
}

// decodeTags 宽松解析标签，避免单条坏数据影响整次检索。
func decodeTags(raw string) []string {
	var payload []any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	out := make([]string, 0, len(payload))
	for _, item := range payload {
		text := trimString(item)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// encodeVector 把向量序列化为 JSON，方便 SQLite 持久化。
func encodeVector(vector []float64) string {
	content, _ := json.Marshal(vector)
	return string(content)
}

// decodeVector 宽松解析向量数据，避免坏记录拖垮整个流程。
func decodeVector(raw string) []float64 {
	var payload []any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	vector := make([]float64, 0, len(payload))
	for _, item := range payload {
		value, ok := toFloat64(item)
		if !ok {
			return nil
		}
		vector = append(vector, value)
	}
	return vector
}

// upsertMemoryEmbedding 统一维护向量写入，减少不同入口间的冲突策略差异。
func upsertMemoryEmbedding(db *sql.DB, memoryID int64, vector []float64, updatedAt string) error {
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := db.Exec(
		`INSERT INTO memory_embeddings (memory_id, vector, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(memory_id) DO UPDATE SET vector = excluded.vector, updated_at = excluded.updated_at`,
		memoryID,
		encodeVector(vector),
		updatedAt,
	)
	return err
}

// deleteAllMemoryEmbeddings 在模型切换时清空旧向量，避免维度混用。
func deleteAllMemoryEmbeddings(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM memory_embeddings`)
	return err
}

// getMemoryMetadata 集中读取元数据，避免上层重复拼 SQL。
func getMemoryMetadata(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM memory_metadata WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// setMemoryMetadata 统一写元数据，保证模型切换状态落在同一处。
func setMemoryMetadata(db *sql.DB, key, value, updatedAt string) error {
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := db.Exec(
		`INSERT INTO memory_metadata (key, value, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key,
		value,
		updatedAt,
	)
	return err
}

// fetchAllMemories 为向量重建提供完整数据集，避免脚本层感知库结构。
func fetchAllMemories(db *sql.DB) ([]MemoryRow, error) {
	return queryMemoryRows(db, `SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at FROM memories ORDER BY timestamp DESC, id DESC`)
}

// fetchMemoryRowsByType 读取指定类型记录，并兼容旧库空项目名数据。
func fetchMemoryRowsByType(db *sql.DB, projectName, memType string, allowLegacyBlankProject bool) ([]MemoryRow, error) {
	query := `SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at FROM memories WHERE project_name = ? AND type = ? ORDER BY timestamp DESC, id DESC`
	args := []any{projectName, memType}
	if allowLegacyBlankProject {
		query = `SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at FROM memories WHERE (project_name = ? OR project_name = '') AND type = ? ORDER BY timestamp DESC, id DESC`
	}
	return queryMemoryRows(db, query, args...)
}

// fetchMemoryEmbeddings 批量读取向量，减少检索阶段数据库往返次数。
func fetchMemoryEmbeddings(db *sql.DB, memoryIDs []int64) (map[int64][]float64, error) {
	if len(memoryIDs) == 0 {
		return map[int64][]float64{}, nil
	}
	placeholders := makePlaceholders(len(memoryIDs))
	args := make([]any, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		args = append(args, id)
	}
	query := fmt.Sprintf(`SELECT memory_id, vector FROM memory_embeddings WHERE memory_id IN (%s)`, placeholders)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]float64{}
	for rows.Next() {
		var memoryID int64
		var raw string
		if err := rows.Scan(&memoryID, &raw); err != nil {
			return nil, err
		}
		if vector := decodeVector(raw); len(vector) > 0 {
			out[memoryID] = vector
		}
	}
	return out, rows.Err()
}

// queryMemoryRows 统一行扫描逻辑，避免多个调用点重复维护字段顺序。
func queryMemoryRows(db *sql.DB, query string, args ...any) ([]MemoryRow, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MemoryRow, 0)
	for rows.Next() {
		var row MemoryRow
		if err := rows.Scan(
			&row.ID,
			&row.ProjectName,
			&row.Type,
			&row.Title,
			&row.Tags,
			&row.Summary,
			&row.Content,
			&row.Timestamp,
			&row.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// makePlaceholders 生成 SQL IN 语句占位符，减少动态 SQL 出错概率。
func makePlaceholders(size int) string {
	if size <= 0 {
		return ""
	}
	result := "?"
	for i := 1; i < size; i++ {
		result += ",?"
	}
	return result
}

// trimString 统一宽松转字符串，避免 JSON 反序列化类型差异污染上层逻辑。
func trimString(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}

// toFloat64 统一向量元素解析，减少不同 JSON 数值类型的兼容分支。
func toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
