#!/usr/bin/env python3
"""将总结或错误记忆写入 SQLite 记忆库。"""

from __future__ import annotations

import argparse
import json
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from embedding_provider import build_memory_embedding_text, create_embedding_provider
from memory_config import resolve_memory_location
from memory_store import connect_memory_db, encode_tags, get_memory_db_path, upsert_memory_embedding
from rebuild_memory_embeddings import rebuild_embeddings


@dataclass(frozen=True)
class MemoryWriteItem:
    """统一描述单条待写入记忆，避免单条和批量入口各自维护字段。"""

    mem_type: str
    title: str
    tags: list[str]
    summary: str
    context: str


def parse_args() -> argparse.Namespace:
    """解析命令行参数，保持对现有调用方式的兼容。"""

    parser = argparse.ArgumentParser(description="Write a memory file")
    parser.add_argument("--root", default=".", help="Project root that contains .memory/")
    parser.add_argument(
        "--items-json",
        default="",
        help="JSON object or array for batch writes, each item includes type/title/tags/summary/context",
    )
    parser.add_argument("--type", choices=["summary", "error"], help="Memory type")
    parser.add_argument("--title", help="Memory title for filename suffix")
    parser.add_argument("--tags", default="", help="Comma-separated tags")
    parser.add_argument("--summary", default="", help="Short synopsis for YAML header")
    parser.add_argument("--context", help="Natural language memory content")
    parser.add_argument(
        "--error-code",
        default="",
        help="Deprecated: ignored. Put original error code directly in --context markdown.",
    )
    parser.add_argument(
        "--fix-code",
        default="",
        help="Deprecated: ignored. Put fixed code directly in --context markdown.",
    )
    return parser.parse_args()


def sanitize_title(title: str) -> str:
    """收敛标题字符集，避免数据库内展示标题过长或混入异常字符。"""

    title = re.sub(r"\s+", "-", title.strip())
    title = re.sub(r"[^0-9A-Za-z_\-\u4e00-\u9fff]", "", title)
    return title[:48] or "memory"


def split_tags(raw: str) -> list[str]:
    """统一清洗标签，避免空标签进入检索索引。"""

    return [tag.strip() for tag in raw.split(",") if tag.strip()]


def normalize_tags(raw: object) -> list[str]:
    """兼容字符串和数组标签写法，减少批量写入时的额外转换成本。"""

    if isinstance(raw, str):
        return split_tags(raw)
    if not isinstance(raw, list):
        return []
    out: list[str] = []
    for item in raw:
        text = str(item).strip()
        if text:
            out.append(text)
    return out


def yaml_header(
    mem_type: str,
    project_name: str,
    title: str,
    tags: list[str],
    summary: str,
) -> str:
    """保留原有 Markdown 头部格式，减少搜索结果结构变化。"""

    tags_line = ", ".join(tags)
    synopsis = summary.strip() or "自动生成记忆"
    return (
        "---\n"
        f"type: {mem_type}\n"
        f"project: {project_name}\n"
        f"title: {title}\n"
        f"tags: {tags_line}\n"
        f"summary: {synopsis}\n"
        "---\n\n"
    )


def markdown_body(context: str) -> str:
    """确保写入内容始终有正文，避免空记录影响后续检索体验。"""

    body = context.strip()
    return body if body else "## Details\n\n暂无内容。"


def build_summary_content(
    project_name: str, title: str, tags: list[str], summary: str, context: str
) -> str:
    """总结记忆继续复用 Markdown 内容，便于片段提取逻辑复用。"""

    return yaml_header("summary", project_name, title, tags, summary) + markdown_body(context) + "\n"


def build_error_content(
    project_name: str,
    title: str,
    tags: list[str],
    summary: str,
    context: str,
    error_code: str,
    fix_code: str,
) -> str:
    """错误记忆与总结记忆共用内容结构，降低维护成本。"""

    _ = (error_code, fix_code)
    return yaml_header("error", project_name, title, tags, summary) + markdown_body(context) + "\n"


def parse_batch_items(args: argparse.Namespace) -> list[MemoryWriteItem]:
    """允许一次请求写入多条记忆，便于按目标或问题拆分总结。"""

    if not args.items_json.strip():
        if not args.type or not args.title or not args.context:
            raise SystemExit("单条写入时必须提供 --type、--title 和 --context")
        return [
            MemoryWriteItem(
                mem_type=args.type,
                title=args.title,
                tags=split_tags(args.tags),
                summary=args.summary.strip(),
                context=args.context,
            )
        ]
    try:
        payload = json.loads(args.items_json)
    except json.JSONDecodeError as exc:
        raise SystemExit("--items-json 必须是合法 JSON") from exc
    if isinstance(payload, dict):
        payload_items = [payload]
    elif isinstance(payload, list):
        payload_items = payload
    else:
        raise SystemExit("--items-json 必须是对象或对象数组")

    items: list[MemoryWriteItem] = []
    for index, item in enumerate(payload_items, start=1):
        if not isinstance(item, dict):
            raise SystemExit(f"--items-json 第 {index} 项必须是对象")
        mem_type = str(item.get("type", "")).strip()
        if mem_type not in {"summary", "error"}:
            raise SystemExit(f"--items-json 第 {index} 项的 type 必须是 summary 或 error")
        title = str(item.get("title", "")).strip()
        if not title:
            raise SystemExit(f"--items-json 第 {index} 项缺少 title")
        context = str(item.get("context", "")).strip()
        if not context:
            raise SystemExit(f"--items-json 第 {index} 项缺少 context")
        items.append(
            MemoryWriteItem(
                mem_type=mem_type,
                title=title,
                tags=normalize_tags(item.get("tags", [])),
                summary=str(item.get("summary", "")).strip(),
                context=context,
            )
        )
    if not items:
        raise SystemExit("--items-json 不能为空数组")
    return items


def main() -> int:
    """根据配置将记忆写入项目内或外挂目录下的 SQLite 文件。"""

    args = parse_args()
    location = resolve_memory_location(Path(args.root))
    rebuild_embeddings(location.project_root)
    memory_root = location.memory_root
    project_name = location.project_name
    provider = create_embedding_provider(location.embedding_config)
    items = parse_batch_items(args)
    now = datetime.now(timezone.utc)
    timestamp_seed = int(now.timestamp())

    with connect_memory_db(memory_root) as conn:
        pending_rows: list[tuple[int, dict[str, object]]] = []
        for index, item in enumerate(items):
            item_now = datetime.fromtimestamp(timestamp_seed + index, timezone.utc)
            ts = item_now.strftime("%Y%m%d%H%M%S")
            title = sanitize_title(item.title)
            tags = item.tags
            if item.mem_type == "summary":
                content = build_summary_content(
                    project_name,
                    title,
                    tags,
                    item.summary,
                    item.context,
                )
            else:
                content = build_error_content(
                    project_name,
                    title,
                    tags,
                    item.summary,
                    item.context,
                    args.error_code,
                    args.fix_code,
                )

            cursor = conn.execute(
                """
                INSERT INTO memories (
                    project_name,
                    type,
                    title,
                    tags,
                    summary,
                    content,
                    timestamp,
                    created_at
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    project_name,
                    item.mem_type,
                    title,
                    encode_tags(tags),
                    item.summary,
                    content,
                    ts,
                    item_now.isoformat(),
                ),
            )
            pending_rows.append(
                (
                    int(cursor.lastrowid),
                    {
                        "project_name": project_name,
                        "type": item.mem_type,
                        "title": title,
                        "tags": encode_tags(tags),
                        "summary": item.summary,
                        "content": content,
                    },
                )
            )

        if provider.enabled and pending_rows:
            vectors = provider.embed_texts(
                [build_memory_embedding_text(row) for _, row in pending_rows]
            )
            for (memory_id, _row), vector in zip(pending_rows, vectors):
                upsert_memory_embedding(
                    conn,
                    memory_id,
                    vector,
                    now.isoformat(),
                )
        conn.commit()

    print(get_memory_db_path(memory_root))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
