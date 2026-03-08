#!/usr/bin/env python3
"""集中管理 SQLite 记忆库，避免读写逻辑散落在多个脚本中。"""

from __future__ import annotations

import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path


def get_memory_db_path(memory_root: Path) -> Path:
    """统一约定记忆数据库路径，避免脚本间路径拼接不一致。"""

    return memory_root / "memory.db"


def connect_memory_db(memory_root: Path) -> sqlite3.Connection:
    """连接并初始化记忆库，保证首次写入前数据库结构已就绪。"""

    memory_root.mkdir(parents=True, exist_ok=True)
    db_path = get_memory_db_path(memory_root)
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    init_memory_db(conn)
    return conn


def init_memory_db(conn: sqlite3.Connection) -> None:
    """初始化表结构与索引，确保检索和按时间排序稳定可用。"""

    conn.executescript(
        """
        CREATE TABLE IF NOT EXISTS memories (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            project_name TEXT NOT NULL DEFAULT '',
            type TEXT NOT NULL CHECK(type IN ('summary', 'error')),
            title TEXT NOT NULL,
            tags TEXT NOT NULL DEFAULT '[]',
            summary TEXT NOT NULL DEFAULT '',
            content TEXT NOT NULL,
            timestamp TEXT NOT NULL,
            created_at TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_memories_type_timestamp
        ON memories(type, timestamp DESC, id DESC);

        CREATE TABLE IF NOT EXISTS memory_embeddings (
            memory_id INTEGER PRIMARY KEY,
            vector TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            FOREIGN KEY(memory_id) REFERENCES memories(id) ON DELETE CASCADE
        );

        CREATE TABLE IF NOT EXISTS memory_metadata (
            key TEXT PRIMARY KEY,
            value TEXT NOT NULL,
            updated_at TEXT NOT NULL
        );
        """
    )
    ensure_project_name_column(conn)
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_memories_project_type_timestamp
        ON memories(project_name, type, timestamp DESC, id DESC)
        """
    )
    conn.commit()


def ensure_project_name_column(conn: sqlite3.Connection) -> None:
    """兼容旧库结构，避免升级后因为缺列导致现有记忆不可读写。"""

    columns = {
        str(row[1]).strip()
        for row in conn.execute("PRAGMA table_info(memories)").fetchall()
        if len(row) > 1
    }
    if "project_name" not in columns:
        conn.execute(
            "ALTER TABLE memories ADD COLUMN project_name TEXT NOT NULL DEFAULT ''"
        )


def encode_tags(tags: list[str]) -> str:
    """将标签序列化为 JSON，减少分隔符规则对内容本身的污染。"""

    return json.dumps(tags, ensure_ascii=False)


def decode_tags(raw: str) -> list[str]:
    """兼容历史异常值，避免单条脏数据导致整次检索失败。"""

    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return []
    if not isinstance(payload, list):
        return []
    return [str(item).strip() for item in payload if str(item).strip()]


def encode_vector(vector: list[float]) -> str:
    """将向量序列化为 JSON，避免 SQLite 缺少原生数组类型带来的歧义。"""

    return json.dumps(vector, ensure_ascii=False, separators=(",", ":"))


def decode_vector(raw: str) -> list[float]:
    """容错解析向量数据，避免单条坏数据中断整个检索流程。"""

    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return []
    if not isinstance(payload, list):
        return []
    vector: list[float] = []
    for item in payload:
        try:
            vector.append(float(item))
        except (TypeError, ValueError):
            return []
    return vector


def upsert_memory_embedding(
    conn: sqlite3.Connection, memory_id: int, vector: list[float], updated_at: str | None = None
) -> None:
    """统一维护记忆向量，避免写入脚本和重建脚本各自处理冲突策略。"""

    timestamp = updated_at or datetime.now(timezone.utc).isoformat()
    conn.execute(
        """
        INSERT INTO memory_embeddings (memory_id, vector, updated_at)
        VALUES (?, ?, ?)
        ON CONFLICT(memory_id) DO UPDATE SET
            vector = excluded.vector,
            updated_at = excluded.updated_at
        """,
        (memory_id, encode_vector(vector), timestamp),
    )


def delete_all_memory_embeddings(conn: sqlite3.Connection) -> None:
    """模型切换时先清空旧向量，避免新旧维度混用导致相似度失真。"""

    conn.execute("DELETE FROM memory_embeddings")


def get_memory_metadata(conn: sqlite3.Connection, key: str) -> str | None:
    """集中读取元数据，避免外部脚本重复拼 SQL。"""

    row = conn.execute(
        "SELECT value FROM memory_metadata WHERE key = ?",
        (key,),
    ).fetchone()
    if row is None:
        return None
    return str(row[0])


def set_memory_metadata(
    conn: sqlite3.Connection, key: str, value: str, updated_at: str | None = None
) -> None:
    """统一写入元数据，确保模型切换和状态同步都落在同一处。"""

    timestamp = updated_at or datetime.now(timezone.utc).isoformat()
    conn.execute(
        """
        INSERT INTO memory_metadata (key, value, updated_at)
        VALUES (?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET
            value = excluded.value,
            updated_at = excluded.updated_at
        """,
        (key, value, timestamp),
    )


def fetch_all_memories(conn: sqlite3.Connection):
    """为重建向量提供完整数据集，避免脚本层感知具体库结构。"""

    return conn.execute(
        """
        SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at
        FROM memories
        ORDER BY timestamp DESC, id DESC
        """
    ).fetchall()


def fetch_memory_embeddings(conn: sqlite3.Connection, memory_ids: list[int]) -> dict[int, list[float]]:
    """批量读取向量，减少检索阶段的数据库往返次数。"""

    if not memory_ids:
        return {}
    placeholders = ",".join("?" for _ in memory_ids)
    rows = conn.execute(
        f"SELECT memory_id, vector FROM memory_embeddings WHERE memory_id IN ({placeholders})",
        tuple(memory_ids),
    ).fetchall()
    out: dict[int, list[float]] = {}
    for row in rows:
        vector = decode_vector(str(row[1]))
        if vector:
            out[int(row[0])] = vector
    return out
