#!/usr/bin/env python3
"""Search memory files under .memory with confidence scoring."""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable

from memory_config import resolve_memory_location


TIMESTAMP_RE = re.compile(r"^(\d{14})")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.+?)\s*$")


@dataclass
class MemoryHit:
    source: str
    path: Path
    timestamp: datetime
    confidence: float
    snippets: list[dict[str, object]]
    file_content: str | None
    header: dict[str, object]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Search memory files under .memory")
    parser.add_argument("--root", default=".", help="Project root that contains .memory/")
    parser.add_argument("--query", required=True, nargs="+", help="Search keyword/regex list")
    parser.add_argument("--error-limit", type=int, default=5, help="Error memories to return")
    parser.add_argument("--summary-limit", type=int, default=10, help="Summary memories to return")
    parser.add_argument(
        "-debug",
        "--debug",
        action="store_true",
        help="Print executed search command(s).",
    )
    return parser.parse_args()


def parse_queries(raw_queries: list[str]) -> list[str]:
    if not raw_queries:
        return []
    # Compatible with both: --query a b  and --query '["a","b"]'
    if len(raw_queries) == 1:
        text = raw_queries[0].strip()
        if text.startswith("[") and text.endswith("]"):
            try:
                value = json.loads(text)
            except json.JSONDecodeError:
                value = None
            if isinstance(value, list):
                parsed = [str(item).strip() for item in value if str(item).strip()]
                if parsed:
                    return parsed
    return [q.strip() for q in raw_queries if q.strip()]


def extract_timestamp(path: Path) -> datetime:
    match = TIMESTAMP_RE.match(path.stem)
    if match:
        return datetime.strptime(match.group(1), "%Y%m%d%H%M%S").replace(tzinfo=timezone.utc)
    return datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc)


def confidence_by_age(ts: datetime, now: datetime) -> float:
    age_days = max(0.0, (now - ts).total_seconds() / 86400.0)
    return math.pow(0.5, age_days / 30.0)


def run_search(
    directory: Path,
    queries: list[str],
    debug_commands: list[str] | None = None,
) -> dict[str, list[int]]:
    if not directory.exists():
        return {}
    if not queries:
        return {}

    rg = ["rg", "-n", "--no-heading", "-i", "--glob", "*.md"]
    for q in queries:
        rg.extend(["-e", q])
    rg.append(str(directory))

    grep = ["grep", "-RIn", "-i", "-E", "--include=*.md"]
    for q in queries:
        grep.extend(["-e", q])
    grep.append(str(directory))
    cmd = rg if shutil_which("rg") else grep
    if debug_commands is not None:
        command_text = shlex.join(cmd)
        debug_commands.append(command_text)
        print(f"[debug] {command_text}", file=sys.stderr)
    proc = subprocess.run(cmd, text=True, capture_output=True, check=False)
    if proc.returncode not in (0, 1):
        raise RuntimeError(proc.stderr.strip() or "search command failed")
    results: dict[str, list[int]] = {}
    for line in proc.stdout.splitlines():
        parts = line.split(":", 2)
        if len(parts) != 3:
            continue
        file_path, line_no, _ = parts
        try:
            parsed_line = int(line_no)
        except ValueError:
            continue
        results.setdefault(file_path, []).append(parsed_line)
    return results


def shutil_which(binary: str) -> bool:
    return any(
        os.access(Path(path) / binary, os.X_OK)
        for path in os.environ.get("PATH", "").split(os.pathsep)
        if path
    )


def collect_hits(
    source: str,
    root: Path,
    queries: list[str],
    limit: int,
    debug_commands: list[str] | None = None,
) -> list[MemoryHit]:
    mapping = run_search(root, queries, debug_commands)
    query_matcher = build_query_matcher(queries)
    now = datetime.now(timezone.utc)
    hits: list[MemoryHit] = []
    for file_path, line_numbers in mapping.items():
        path = Path(file_path)
        lines = read_lines(path)
        header = read_header(lines)
        body_start = body_start_index(lines)
        header_match_lines = header_match_line_numbers(line_numbers, body_start)
        header_line_match = bool(header_match_lines)
        header_title_match = match_header_title(header, query_matcher)
        header_field_match = match_header_fields(header, query_matcher)
        body_match_lines = body_match_line_numbers(line_numbers, body_start)
        if not body_match_lines and not header_line_match and not header_field_match:
            continue
        snippets: list[dict[str, object]] = []
        file_content: str | None = None
        if header_title_match:
            file_content = build_full_file_content(lines)
        elif body_match_lines:
            snippets = build_body_section_snippets(lines, body_start, body_match_lines, query_matcher)
        if not snippets and file_content is None:
            snippets = build_header_snippets(lines, header, query_matcher, header_match_lines)
        if not snippets and file_content is None:
            continue
        ts = extract_timestamp(path)
        hits.append(
            MemoryHit(
                source=source,
                path=path,
                timestamp=ts,
                confidence=confidence_by_age(ts, now),
                snippets=snippets,
                file_content=file_content,
                header=header,
            )
        )
    hits.sort(key=lambda x: x.timestamp, reverse=True)
    return hits[:limit]


def to_dicts(hits: Iterable[MemoryHit]) -> list[dict[str, object]]:
    payload = []
    for h in hits:
        payload.append(
            {
                "source": h.source,
                "path": str(h.path),
                "timestamp": h.timestamp.isoformat(),
                "confidence": round(h.confidence, 3),
                "snippets": h.snippets,
                "file_content": h.file_content,
                "header": h.header,
            }
        )
    return payload


def markdown_fence_for(text: str) -> str:
    return "````" if "```" in text else "```"


def render_hit_markdown(hit: dict[str, object], index: int) -> str:
    lines: list[str] = []
    lines.append(f"### Record {index}")
    source = str(hit.get("source", "")).strip()
    path = str(hit.get("path", "")).strip()
    timestamp = str(hit.get("timestamp", "")).strip()
    confidence = hit.get("confidence")
    if source:
        lines.append(f"- source: {source}")
    if path:
        lines.append(f"- path: {path}")
    if timestamp:
        lines.append(f"- timestamp: {timestamp}")
    if confidence not in ("", None):
        lines.append(f"- confidence: {confidence}")

    file_content = hit.get("file_content")
    has_file_content = isinstance(file_content, str) and bool(file_content)

    header = hit.get("header", {})
    header_lines: list[str] = []
    if not has_file_content and isinstance(header, dict):
        title = header.get("title", "")
        summary = header.get("summary", "")
        tags = header.get("tags", [])
        if isinstance(title, str) and title.strip():
            header_lines.append(f"  - title: {title}")
        if isinstance(tags, list):
            tag_text = ", ".join(str(t) for t in tags if str(t).strip())
            if tag_text:
                header_lines.append(f"  - tags: {tag_text}")
        elif str(tags).strip():
            header_lines.append(f"  - tags: {tags}")
        if isinstance(summary, str) and summary.strip():
            header_lines.append(f"  - summary: {summary}")
    elif not has_file_content:
        text = str(header).strip()
        if text:
            header_lines.append(f"  - {text}")

    if header_lines:
        lines.append("- header:")
        lines.extend(header_lines)

    snippets = hit.get("snippets", [])
    if has_file_content:
        fence = markdown_fence_for(file_content)
        lines.append("- file_content:")
        lines.append(f"  {fence}markdown")
        for content_line in file_content.splitlines():
            lines.append(f"  {content_line}")
        lines.append(f"  {fence}")

    if isinstance(snippets, list) and snippets:
        lines.append("- snippets:")
        for snippet in snippets:
            line_range = snippet.get("line_range", {})
            start = ""
            end = ""
            if isinstance(line_range, dict):
                start = str(line_range.get("start", ""))
                end = str(line_range.get("end", ""))
            content = str(snippet.get("content", ""))
            fence = markdown_fence_for(content)
            lines.append(f"  - line_range: {start}-{end}")
            lines.append("    content:")
            lines.append(f"    {fence}markdown")
            for content_line in content.splitlines():
                lines.append(f"    {content_line}")
            lines.append(f"    {fence}")

    return "\n".join(lines)


def render_hits_section(title: str, hits: list[dict[str, object]]) -> str:
    lines: list[str] = [f"## {title} ({len(hits)})"]
    if not hits:
        lines.append("- (none)")
        return "\n".join(lines)

    for idx, hit in enumerate(hits, start=1):
        lines.append(render_hit_markdown(hit, idx))
        if idx < len(hits):
            lines.append("---")
    return "\n".join(lines)


def render_result_markdown(
    query: str,
    search_root: str,
    error_hits: list[dict[str, object]],
    summary_hits: list[dict[str, object]],
    debug_commands: list[str] | None = None,
) -> str:
    lines = [
        "# Memory Search Result",
        f"- query: {query}",
        f"- search_root: {search_root}",
    ]
    if debug_commands:
        lines.append("- debug: true")
        lines.append("")
        lines.append("## Debug Commands")
        for cmd in debug_commands:
            lines.append(f"- `{cmd}`")
    lines.extend(
        [
            "",
            render_hits_section("Error Hits", error_hits),
            "",
            render_hits_section("Summary Hits", summary_hits),
        ]
    )
    return "\n".join(lines).strip() + "\n"


def read_lines(path: Path) -> list[str]:
    try:
        return path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []


def body_start_index(lines: list[str]) -> int:
    if not lines:
        return 0
    if lines[0].strip() != "---":
        return 0
    for idx in range(1, len(lines)):
        if lines[idx].strip() == "---":
            return idx + 1
    return 0


def parse_scalar(value: str) -> object:
    text = value.strip()
    if len(text) >= 2 and text[0] == '"' and text[-1] == '"':
        return text[1:-1].replace('\\"', '"').replace("\\\\", "\\")
    if len(text) >= 2 and text[0] == "'" and text[-1] == "'":
        return text[1:-1]
    return text


def parse_tags(value: str) -> list[str]:
    text = value.strip()
    if text.startswith("[") and text.endswith("]"):
        inner = text[1:-1].strip()
    else:
        inner = text
    if not inner:
        return []
    tags: list[str] = []
    for part in inner.split(","):
        v = parse_scalar(part)
        if isinstance(v, str) and v:
            tags.append(v)
    return tags


def build_query_matcher(queries: list[str]):
    patterns: list[re.Pattern[str]] = []
    lowered_fallback: list[str] = []
    for q in queries:
        try:
            patterns.append(re.compile(q, re.IGNORECASE))
        except re.error:
            lowered_fallback.append(q.lower())

    def matcher(text: str) -> bool:
        for p in patterns:
            if p.search(text):
                return True
        lowered = text.lower()
        for q in lowered_fallback:
            if q in lowered:
                return True
        return False

    return matcher


def match_header_fields(header: dict[str, object], matcher) -> bool:
    for key in ("title", "summary"):
        value = header.get(key)
        if isinstance(value, str) and value and matcher(value):
            return True

    tags = header.get("tags")
    if isinstance(tags, list):
        for tag in tags:
            if isinstance(tag, str) and tag and matcher(tag):
                return True
    return False


def match_header_title(header: dict[str, object], matcher) -> bool:
    title = header.get("title")
    return isinstance(title, str) and bool(title) and matcher(title)


def read_header(lines: list[str]) -> dict[str, object]:
    if not lines or lines[0].strip() != "---":
        return {}
    end_idx = -1
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            end_idx = i
            break
    if end_idx == -1:
        return {}

    header: dict[str, object] = {}
    for line in lines[1:end_idx]:
        if ":" not in line:
            continue
        key, value = line.split(":", 1)
        key = key.strip()
        raw = value.strip()
        if key == "tags":
            header[key] = parse_tags(raw)
        else:
            header[key] = parse_scalar(raw)
    return header


def body_match_line_numbers(line_numbers: list[int], body_start: int) -> list[int]:
    out: list[int] = []
    for line_no in sorted(set(line_numbers)):
        if line_no - 1 >= body_start:
            out.append(line_no)
    return out


def header_match_line_numbers(line_numbers: list[int], body_start: int) -> list[int]:
    out: list[int] = []
    for line_no in sorted(set(line_numbers)):
        if line_no - 1 < body_start:
            out.append(line_no)
    return out


def is_heading_line(line: str) -> bool:
    return HEADING_RE.match(line.strip()) is not None


def heading_level(line: str) -> int:
    match = HEADING_RE.match(line.strip())
    if not match:
        return 0
    return len(match.group(1))


def heading_text(line: str) -> str:
    match = HEADING_RE.match(line.strip())
    if not match:
        return ""
    return match.group(2).strip()


def section_end_index(body_lines: list[str], heading_idx: int) -> int:
    start_level = heading_level(body_lines[heading_idx])
    for idx in range(heading_idx + 1, len(body_lines)):
        if not is_heading_line(body_lines[idx]):
            continue
        if heading_level(body_lines[idx]) <= start_level:
            return idx
    return len(body_lines)


def extract_section(body_lines: list[str], heading_idx: int) -> tuple[str, int, int]:
    end_idx = section_end_index(body_lines, heading_idx)
    content = "\n".join(body_lines[heading_idx:end_idx]).strip()
    return content, heading_idx, end_idx


def find_parent_heading_index(body_lines: list[str], line_idx: int) -> int | None:
    for idx in range(line_idx, -1, -1):
        if is_heading_line(body_lines[idx]):
            return idx
    return None


def make_snippet(start_line: int, end_line: int, content: str) -> dict[str, object]:
    return {
        "line_range": {"start": start_line, "end": end_line},
        "content": content,
    }


def build_full_file_content(lines: list[str]) -> str:
    return "\n".join(lines).strip()


def build_body_section_snippets(
    lines: list[str], body_start: int, match_lines: list[int], matcher
) -> list[dict[str, object]]:
    body_lines = lines[body_start:]
    if not body_lines:
        return []

    match_indices = [line_no - 1 - body_start for line_no in match_lines]
    match_indices = [idx for idx in match_indices if 0 <= idx < len(body_lines)]
    if not match_indices:
        return []

    snippets: list[dict[str, object]] = []
    seen_ranges: set[tuple[int, int]] = set()

    for idx in match_indices:
        content = ""
        start_idx = 0
        end_idx = 0

        line = body_lines[idx]
        if is_heading_line(line) and matcher(heading_text(line)):
            content, start_idx, end_idx = extract_section(body_lines, idx)
        else:
            parent_heading_idx = find_parent_heading_index(body_lines, idx)
            if parent_heading_idx is not None:
                content, start_idx, end_idx = extract_section(body_lines, parent_heading_idx)
            else:
                abs_match_line = body_start + idx + 1
                content, start_idx, end_idx = build_body_snippet(lines, body_start, abs_match_line)

        if not content:
            continue
        abs_start = body_start + start_idx + 1
        abs_end = body_start + end_idx
        key = (abs_start, abs_end)
        if key in seen_ranges:
            continue
        seen_ranges.add(key)
        snippets.append(make_snippet(abs_start, abs_end, content))

    return snippets


def build_body_snippet(lines: list[str], body_start: int, match_line: int) -> tuple[str, int, int]:
    body_lines = lines[body_start:]
    if not body_lines:
        return "", 0, 0

    match_idx = match_line - 1 - body_start
    if match_idx < 0 or match_idx >= len(body_lines):
        return "", 0, 0

    before = min(10, match_idx)
    after = min(9, len(body_lines) - match_idx - 1)
    total = before + 1 + after
    missing = 20 - total

    if missing > 0:
        extra_after = min(missing, len(body_lines) - match_idx - 1 - after)
        after += extra_after
        missing -= extra_after

    if missing > 0:
        extra_before = min(missing, match_idx - before)
        before += extra_before

    start = match_idx - before
    end = match_idx + after + 1
    return "\n".join(body_lines[start:end]).strip(), start, end


def build_header_snippets(
    lines: list[str],
    header: dict[str, object],
    matcher,
    header_match_lines: list[int],
) -> list[dict[str, object]]:
    if not lines or lines[0].strip() != "---":
        return []

    end_idx = -1
    for idx in range(1, len(lines)):
        if lines[idx].strip() == "---":
            end_idx = idx
            break
    if end_idx == -1:
        return []

    snippets: list[dict[str, object]] = []
    for line_no in range(2, end_idx + 1):
        raw = lines[line_no - 1]
        if ":" not in raw:
            continue
        key, value = raw.split(":", 1)
        key = key.strip()
        value = value.strip()
        if not value:
            continue
        if key in ("title", "summary") and matcher(value):
            snippets.append(make_snippet(line_no, line_no, f"{key}: {value}"))
        if key == "tags":
            tag_values = parse_tags(value)
            matched_tags = [tag for tag in tag_values if matcher(tag)]
            if matched_tags:
                snippets.append(make_snippet(line_no, line_no, f"tags: {', '.join(matched_tags)}"))

    if snippets:
        return snippets

    for line_no in header_match_lines:
        if 1 <= line_no <= len(lines):
            content = lines[line_no - 1].strip()
            if content:
                snippets.append(make_snippet(line_no, line_no, content))
    if snippets:
        return snippets

    fallback: list[str] = []
    title = header.get("title")
    if isinstance(title, str) and title:
        fallback.append(f"title: {title}")
    tags = header.get("tags")
    if isinstance(tags, list) and tags:
        fallback.append(f"tags: {', '.join(str(tag) for tag in tags)}")
    summary = header.get("summary")
    if isinstance(summary, str) and summary:
        fallback.append(f"summary: {summary}")
    if not fallback:
        return []
    return [make_snippet(1, end_idx + 1, "\n".join(fallback))]


def main() -> int:
    """按配置解析记忆目录后执行搜索。"""

    args = parse_args()
    queries = parse_queries(args.query)
    if not queries:
        raise SystemExit("--query must contain at least one non-empty keyword")
    location = resolve_memory_location(Path(args.root))
    memory_root = location.memory_root
    errors_root = memory_root / "errors"
    summaries_root = memory_root / "summaries"

    debug_commands: list[str] | None = [] if args.debug else None
    error_hits = collect_hits("error", errors_root, queries, args.error_limit, debug_commands)
    summary_hits = collect_hits("summary", summaries_root, queries, args.summary_limit, debug_commands)
    error_hit_dicts = to_dicts(error_hits)
    summary_hit_dicts = to_dicts(summary_hits)
    query_text = ", ".join(queries)
    print(
        render_result_markdown(
            query_text,
            str(location.search_root),
            error_hit_dicts,
            summary_hit_dicts,
            debug_commands,
        ),
        end="",
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
