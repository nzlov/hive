package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DBPath 统一数据库文件位置，避免服务层和命令层路径拼接不一致。
func DBPath(memoryRoot string) string {
	return filepath.Join(memoryRoot, "memory.db")
}

// ConnectDB 负责连接并初始化数据库，确保首次使用时表结构已经就绪。
func ConnectDB(memoryRoot string) (*sql.DB, error) {
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", DBPath(memoryRoot))
	if err != nil {
		return nil, err
	}
	if err := initDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// initDB 初始化表结构和索引，保证检索排序与向量存储稳定可用。
func initDB(db *sql.DB) error {
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
		`CREATE INDEX IF NOT EXISTS idx_memories_project_type_timestamp ON memories(project_name, type, timestamp DESC, id DESC);`,
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
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return ensureProjectNameColumn(db)
}

// ensureProjectNameColumn 兼容旧库结构，避免升级后因为缺列导致现有记忆不可读写。
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

// EncodeTags 使用 JSON 保存标签，避免分隔符规则污染实际内容。
func EncodeTags(tags []string) string {
	data, _ := json.Marshal(tags)
	return string(data)
}

// DecodeTags 宽松解析标签，避免单条脏数据中断整次检索流程。
func DecodeTags(raw string) []string {
	var payload []any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	out := make([]string, 0, len(payload))
	for _, item := range payload {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// encodeVector 序列化向量，避免 SQLite 缺少原生数组类型带来的歧义。
func encodeVector(vector []float64) string {
	data, _ := json.Marshal(vector)
	return string(data)
}

// decodeVector 宽松解析向量数据，避免单条坏数据拖垮整次检索。
func decodeVector(raw string) []float64 {
	var payload []float64
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return payload
}

// InsertMemory 写入一条记忆并返回主键，为后续向量写入提供关联键。
func InsertMemory(tx *sql.Tx, row Row) (int64, error) {
	result, err := tx.Exec(
		`INSERT INTO memories (project_name, type, title, tags, summary, content, timestamp, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ProjectName,
		row.Type,
		row.Title,
		row.Tags,
		row.Summary,
		row.Content,
		row.Timestamp,
		row.CreatedAt,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpsertMemoryEmbedding 统一维护向量写入，避免写入和重建各自处理冲突策略。
func UpsertMemoryEmbedding(tx *sql.Tx, memoryID int64, vector []float64, updatedAt string) error {
	_, err := tx.Exec(
		`INSERT INTO memory_embeddings (memory_id, vector, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(memory_id) DO UPDATE SET vector = excluded.vector, updated_at = excluded.updated_at`,
		memoryID,
		encodeVector(vector),
		updatedAt,
	)
	return err
}

// DeleteAllMemoryEmbeddings 在模型切换时先清空旧向量，避免新旧维度混用导致相似度失真。
func DeleteAllMemoryEmbeddings(tx *sql.Tx) error {
	_, err := tx.Exec(`DELETE FROM memory_embeddings`)
	return err
}

// GetMemoryMetadata 统一读取元数据，减少服务层重复拼接 SQL。
func GetMemoryMetadata(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM memory_metadata WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetMemoryMetadata 统一写入元数据，确保模型切换状态同步落在同一处。
func SetMemoryMetadata(tx *sql.Tx, key, value, updatedAt string) error {
	_, err := tx.Exec(
		`INSERT INTO memory_metadata (key, value, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key,
		value,
		updatedAt,
	)
	return err
}

// FetchAllMemories 为向量重建提供完整数据集，避免服务层感知底层 SQL 细节。
func FetchAllMemories(db *sql.DB) ([]Row, error) {
	rows, err := db.Query(`SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at FROM memories ORDER BY timestamp DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// FetchMemoryRows 按项目和类型读取候选记录，让搜索逻辑只关注匹配规则本身。
func FetchMemoryRows(db *sql.DB, projectName, memType string, allowLegacyBlankProject bool) ([]Row, error) {
	query := `SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at FROM memories WHERE project_name = ? AND type = ? ORDER BY timestamp DESC, id DESC`
	args := []any{projectName, memType}
	if allowLegacyBlankProject {
		query = `SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at FROM memories WHERE (project_name = ? OR project_name = '') AND type = ? ORDER BY timestamp DESC, id DESC`
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// FetchMemoryEmbeddings 批量读取向量，减少检索阶段数据库往返次数。
func FetchMemoryEmbeddings(db *sql.DB, memoryIDs []int64) (map[int64][]float64, error) {
	if len(memoryIDs) == 0 {
		return map[int64][]float64{}, nil
	}
	placeholders := make([]string, 0, len(memoryIDs))
	args := make([]any, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	query := `SELECT memory_id, vector FROM memory_embeddings WHERE memory_id IN (` + strings.Join(placeholders, ",") + `)`
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

// scanRows 集中做行对象转换，避免不同查询重复维护字段顺序。
func scanRows(rows *sql.Rows) ([]Row, error) {
	out := []Row{}
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.ID, &row.ProjectName, &row.Type, &row.Title, &row.Tags, &row.Summary, &row.Content, &row.Timestamp, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
