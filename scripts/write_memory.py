#!/usr/bin/env python3
"""将总结或错误记忆写入 SQLite 记忆库。"""

from __future__ import annotations

import argparse
import re
from datetime import datetime, timezone
from pathlib import Path

from memory_config import resolve_memory_location
from memory_store import connect_memory_db, encode_tags, get_memory_db_path


def parse_args() -> argparse.Namespace:
    """解析命令行参数，保持对现有调用方式的兼容。"""

    parser = argparse.ArgumentParser(description="Write a memory file")
    parser.add_argument("--root", default=".", help="Project root that contains .memory/")
    parser.add_argument("--type", choices=["summary", "error"], required=True)
    parser.add_argument("--title", required=True, help="Memory title for filename suffix")
    parser.add_argument("--tags", default="", help="Comma-separated tags")
    parser.add_argument("--summary", default="", help="Short synopsis for YAML header")
    parser.add_argument("--context", required=True, help="Natural language memory content")
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


def main() -> int:
    """根据配置将记忆写入项目内或外挂目录下的 SQLite 文件。"""

    args = parse_args()
    location = resolve_memory_location(Path(args.root))
    memory_root = location.memory_root
    project_name = location.project_name
    now = datetime.now(timezone.utc)
    ts = now.strftime("%Y%m%d%H%M%S")
    title = sanitize_title(args.title)
    tags = split_tags(args.tags)

    if args.type == "summary":
        content = build_summary_content(project_name, title, tags, args.summary, args.context)
    else:
        content = build_error_content(
            project_name,
            title,
            tags,
            args.summary,
            args.context,
            args.error_code,
            args.fix_code,
        )

    with connect_memory_db(memory_root) as conn:
        conn.execute(
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
                args.type,
                title,
                encode_tags(tags),
                args.summary.strip(),
                content,
                ts,
                now.isoformat(),
            ),
        )
        conn.commit()

    print(get_memory_db_path(memory_root))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
