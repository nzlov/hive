#!/usr/bin/env python3
"""重建记忆向量，供模型切换和手动维护场景复用。"""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
from pathlib import Path

from embedding_provider import build_memory_embedding_text, create_embedding_provider
from memory_config import resolve_memory_location
from memory_store import (
    connect_memory_db,
    delete_all_memory_embeddings,
    fetch_all_memories,
    get_memory_metadata,
    set_memory_metadata,
    upsert_memory_embedding,
)


EMBEDDING_MODEL_META_KEY = "embedding_model"


def parse_args() -> argparse.Namespace:
    """保留独立脚本入口，便于自动触发和手动运维复用同一逻辑。"""

    parser = argparse.ArgumentParser(description="Rebuild memory embeddings")
    parser.add_argument("--root", default=".", help="Project root that contains .memory/")
    parser.add_argument(
        "--force",
        action="store_true",
        help="Rebuild even when configured model matches database metadata",
    )
    return parser.parse_args()


def rebuild_embeddings(project_root: Path, force: bool = False) -> tuple[bool, str]:
    """在模型变化时全量重建向量，避免维度切换后产生混合索引。"""

    location = resolve_memory_location(project_root)
    provider = create_embedding_provider(location.embedding_config)
    if not provider.enabled:
        return False, "未配置嵌入模型，跳过重建。"

    with connect_memory_db(location.memory_root) as conn:
        current_model = get_memory_metadata(conn, EMBEDDING_MODEL_META_KEY)
        rows = fetch_all_memories(conn)
        embedding_count = int(
            conn.execute("SELECT COUNT(*) FROM memory_embeddings").fetchone()[0]
        )
        if current_model == provider.model_name and embedding_count == len(rows) and not force:
            return False, f"嵌入模型未变化，继续使用 {provider.model_name}。"

        texts = [build_memory_embedding_text(row) for row in rows]
        vectors = provider.embed_texts(texts) if texts else []
        delete_all_memory_embeddings(conn)
        rebuilt_at = datetime.now(timezone.utc).isoformat()
        for row, vector in zip(rows, vectors):
            upsert_memory_embedding(conn, int(row["id"]), vector, rebuilt_at)
        set_memory_metadata(conn, EMBEDDING_MODEL_META_KEY, provider.model_name, rebuilt_at)
        conn.commit()
    return True, f"已使用模型 {provider.model_name} 重建 {len(vectors)} 条向量。"


def main() -> int:
    """提供命令行入口，便于脚本启动阶段自动调用。"""

    args = parse_args()
    _, message = rebuild_embeddings(Path(args.root), force=args.force)
    print(message)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
