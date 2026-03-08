#!/usr/bin/env python3
"""Write summary/error memory files under .memory."""

from __future__ import annotations

import argparse
import re
from datetime import datetime
from pathlib import Path


def parse_args() -> argparse.Namespace:
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
    title = re.sub(r"\s+", "-", title.strip())
    title = re.sub(r"[^0-9A-Za-z_\-\u4e00-\u9fff]", "", title)
    return title[:48] or "memory"


def split_tags(raw: str) -> list[str]:
    return [tag.strip() for tag in raw.split(",") if tag.strip()]


def yaml_header(
    mem_type: str,
    title: str,
    tags: list[str],
    summary: str,
) -> str:
    tags_line = ", ".join(tags)
    synopsis = summary.strip() or "自动生成记忆"
    return (
        "---\n"
        f"type: {mem_type}\n"
        f"title: {title}\n"
        f"tags: {tags_line}\n"
        f"summary: {synopsis}\n"
        "---\n\n"
    )


def markdown_body(context: str) -> str:
    body = context.strip()
    return body if body else "## Details\n\n暂无内容。"


def build_summary_content(
    title: str, tags: list[str], summary: str, context: str
) -> str:
    return yaml_header("summary", title, tags, summary) + markdown_body(context) + "\n"


def build_error_content(
    title: str,
    tags: list[str],
    summary: str,
    context: str,
    error_code: str,
    fix_code: str,
) -> str:
    _ = (error_code, fix_code)
    return yaml_header("error", title, tags, summary) + markdown_body(context) + "\n"


def main() -> int:
    args = parse_args()
    root = Path(args.root).resolve()
    memory_root = root / ".memory"
    now = datetime.now()
    year = now.strftime("%Y")
    month = now.strftime("%m")
    target_dir = memory_root / ("summaries" if args.type == "summary" else "errors") / year / month
    target_dir.mkdir(parents=True, exist_ok=True)

    ts = now.strftime("%Y%m%d%H%M%S")
    title = sanitize_title(args.title)
    file_path = target_dir / f"{ts}{title}.md"
    tags = split_tags(args.tags)

    if args.type == "summary":
        content = build_summary_content(title, tags, args.summary, args.context)
    else:
        content = build_error_content(
            title,
            tags,
            args.summary,
            args.context,
            args.error_code,
            args.fix_code,
        )

    file_path.write_text(content, encoding="utf-8")
    print(file_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
