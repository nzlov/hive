#!/usr/bin/env python3
"""集中管理 SQLite 记忆库，避免读写逻辑散落在多个脚本中。"""

from __future__ import annotations

import json
import sqlite3
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
