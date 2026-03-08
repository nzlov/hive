#!/usr/bin/env python3
"""从 SQLite 记忆库中检索记忆并输出统一 Markdown 结果。"""

from __future__ import annotations

import argparse
import json
import math
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable

from embedding_provider import build_query_embedding_text, cosine_similarity, create_embedding_provider
from memory_config import resolve_memory_location
from memory_store import connect_memory_db, fetch_memory_embeddings, get_memory_db_path
from rebuild_memory_embeddings import rebuild_embeddings


TIMESTAMP_RE = re.compile(r"^(\d{14})")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.+?)\s*$")


@dataclass
class MemoryHit:
    id: int
    source: str
    path: str
    project_name: str
    timestamp: datetime
    confidence: float
    snippets: list[dict[str, object]]
    file_content: str | None
    header: dict[str, object]


def parse_args() -> argparse.Namespace:
    """解析命令行参数，保持现有调用入口稳定。"""

    parser = argparse.ArgumentParser(description="Search memory files under .memory")
    parser.add_argument("--root", default=".", help="Project root that contains .memory/")
    parser.add_argument("--query", required=True, nargs="+", help="Search keyword/regex list")
    parser.add_argument(
        "-debug",
        "--debug",
        action="store_true",
        help="Print executed search command(s).",
    )
    return parser.parse_args()


def parse_queries(raw_queries: list[str]) -> list[str]:
    """兼容数组和多参数写法，避免调用方因参数格式不同而失败。"""

    if not raw_queries:
        return []
    # 兼容 `--query a b` 和 `--query '["a","b"]'` 两种调用方式，减少调用方改造成本。
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


def extract_timestamp(timestamp: str) -> datetime:
    """优先使用数据库内时间戳，保证排序与展示一致。"""

    match = TIMESTAMP_RE.match(timestamp.strip())
    if match:
        return datetime.strptime(match.group(1), "%Y%m%d%H%M%S").replace(tzinfo=timezone.utc)
    return datetime.now(timezone.utc)


def confidence_by_age(ts: datetime, now: datetime) -> float:
    """沿用时间衰减规则，让旧记忆自然降权而不是直接丢弃。"""

    age_days = max(0.0, (now - ts).total_seconds() / 86400.0)
    return math.pow(0.5, age_days / 30.0)


def match_line_numbers(lines: list[str], matcher) -> list[int]:
    """按行匹配全文，继续复用原有片段截取逻辑。"""

    matched: list[int] = []
    for idx, line in enumerate(lines, start=1):
        if matcher(line):
            matched.append(idx)
    return matched


def fetch_memory_rows(
    memory_root: Path,
    mem_type: str,
    project_name: str,
    allow_legacy_blank_project: bool,
    debug_commands: list[str] | None = None,
):
    """读取指定类型的记忆记录，避免搜索脚本直接耦合 SQL 细节。"""

    if debug_commands is not None:
        debug_commands.append(
            f"sqlite scan: {get_memory_db_path(memory_root)} [{project_name}/{mem_type}]"
        )
    with connect_memory_db(memory_root) as conn:
        where_clause = "(project_name = ? OR project_name = '') AND type = ?"
        params: tuple[str, str] = (project_name, mem_type)
        if not allow_legacy_blank_project:
            where_clause = "project_name = ? AND type = ?"
        return conn.execute(
            """
            SELECT id, project_name, type, title, tags, summary, content, timestamp, created_at
            FROM memories
            WHERE """
            + where_clause
            + """
            ORDER BY timestamp DESC, id DESC
            """,
            params,
        ).fetchall()


def collect_hits(
    source: str,
    memory_root: Path,
    project_name: str,
    allow_legacy_blank_project: bool,
    queries: list[str],
    debug_commands: list[str] | None = None,
) -> list[MemoryHit]:
    """在数据库记录中筛选命中项，并保持旧输出结构不变。"""

    query_matcher = build_query_matcher(queries)
    now = datetime.now(timezone.utc)
    hits: list[MemoryHit] = []
    db_path = str(get_memory_db_path(memory_root))
    for row in fetch_memory_rows(
        memory_root,
        source,
        project_name,
        allow_legacy_blank_project,
        debug_commands,
    ):
        lines = str(row["content"] or "").splitlines()
        header = read_header(lines)
        body_start = body_start_index(lines)
        line_numbers = match_line_numbers(lines, query_matcher)
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
        ts = extract_timestamp(str(row["timestamp"] or ""))
        hits.append(
            MemoryHit(
                id=int(row["id"]),
                source=source,
                path=f"{db_path}#project={row['project_name']}#id={row['id']}",
                project_name=str(row["project_name"] or ""),
                timestamp=ts,
                confidence=confidence_by_age(ts, now),
                snippets=snippets,
                file_content=file_content,
                header=header,
            )
        )
    hits.sort(key=lambda x: x.timestamp, reverse=True)
    return hits


def collect_semantic_hits(
    source: str,
    memory_root: Path,
    project_name: str,
    allow_legacy_blank_project: bool,
    queries: list[str],
    provider,
    debug_commands: list[str] | None = None,
) -> list[MemoryHit]:
    """在保留关键字检索的同时补充语义召回，减少措辞变化带来的漏检。"""

    if not provider.enabled:
        return []
    query_text = build_query_embedding_text(source, queries)
    if not query_text:
        return []
    query_vector = provider.embed_texts([query_text])[0]
    rows = fetch_memory_rows(
        memory_root,
        source,
        project_name,
        allow_legacy_blank_project,
        debug_commands,
    )
    memory_ids = [int(row["id"]) for row in rows]
    with connect_memory_db(memory_root) as conn:
        embedding_map = fetch_memory_embeddings(conn, memory_ids)
    now = datetime.now(timezone.utc)
    db_path = str(get_memory_db_path(memory_root))
    hits: list[MemoryHit] = []
    for row in rows:
        memory_id = int(row["id"])
        vector = embedding_map.get(memory_id)
        if not vector:
            continue
        semantic_score = cosine_similarity(query_vector, vector)
        if semantic_score <= 0.15:
            continue
        lines = str(row["content"] or "").splitlines()
        header = read_header(lines)
        ts = extract_timestamp(str(row["timestamp"] or ""))
        age_score = confidence_by_age(ts, now)
        hits.append(
            MemoryHit(
                id=memory_id,
                source=source,
                path=f"{db_path}#project={row['project_name']}#id={row['id']}",
                project_name=str(row["project_name"] or ""),
                timestamp=ts,
                confidence=max(semantic_score, age_score * 0.5 + semantic_score * 0.5),
                snippets=[],
                file_content=build_full_file_content(lines),
                header=header,
            )
        )
    hits.sort(key=lambda hit: (hit.confidence, hit.timestamp), reverse=True)
    return hits


def merge_hits(primary_hits: list[MemoryHit], semantic_hits: list[MemoryHit]) -> list[MemoryHit]:
    """关键字命中优先保留原片段展示，再补上语义召回缺失的结果。"""

    merged: list[MemoryHit] = []
    seen_ids: set[int] = set()
    semantic_by_id = {hit.id: hit for hit in semantic_hits}

    for hit in primary_hits:
        semantic_hit = semantic_by_id.get(hit.id)
        if semantic_hit is not None and semantic_hit.confidence > hit.confidence:
            hit.confidence = semantic_hit.confidence
            if hit.file_content is None:
                hit.file_content = semantic_hit.file_content
        merged.append(hit)
        seen_ids.add(hit.id)

    for hit in semantic_hits:
        if hit.id in seen_ids:
            continue
        merged.append(hit)
        seen_ids.add(hit.id)
    return merged


def to_dicts(hits: Iterable[MemoryHit]) -> list[dict[str, object]]:
    """序列化命中结果，供统一 Markdown 渲染层使用。"""

    payload = []
    for h in hits:
        payload.append(
            {
                "source": h.source,
                "path": str(h.path),
                "project_name": h.project_name,
                "timestamp": h.timestamp.isoformat(),
                "confidence": round(h.confidence, 3),
                "snippets": h.snippets,
                "file_content": h.file_content,
                "header": h.header,
            }
        )
    return payload


def markdown_fence_for(text: str) -> str:
    """根据正文内容选择围栏长度，避免嵌套代码块被截断。"""

    return "````" if "```" in text else "```"


def render_hit_markdown(hit: dict[str, object], index: int) -> str:
    """渲染单条命中结果，保持 CLI 输出稳定且可扫描。"""

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
    project_name = str(hit.get("project_name", "")).strip()
    if project_name:
        lines.append(f"- project: {project_name}")
    if timestamp:
        lines.append(f"- timestamp: {timestamp}")
    if confidence not in ("", None):
        lines.append(f"- confidence: {confidence}")

    file_content = hit.get("file_content")
    has_file_content = isinstance(file_content, str) and bool(file_content)

    header = hit.get("header", {})
    header_lines: list[str] = []
    if not has_file_content and isinstance(header, dict):
        project = header.get("project", "")
        title = header.get("title", "")
        summary = header.get("summary", "")
        tags = header.get("tags", [])
        if isinstance(project, str) and project.strip():
            header_lines.append(f"  - project: {project}")
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
    """渲染分类结果，空结果也显式展示避免歧义。"""

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
    """输出最终 Markdown，兼容现有 skill 的结果消费方式。"""

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


def body_start_index(lines: list[str]) -> int:
    """定位 YAML 头部结束位置，便于区分元数据与正文匹配。"""

    if not lines:
        return 0
    if lines[0].strip() != "---":
        return 0
    for idx in range(1, len(lines)):
        if lines[idx].strip() == "---":
            return idx + 1
    return 0


def parse_scalar(value: str) -> object:
    """按轻量规则解析头部值，避免强依赖完整 YAML 解析器。"""

    text = value.strip()
    if len(text) >= 2 and text[0] == '"' and text[-1] == '"':
        return text[1:-1].replace('\\"', '"').replace("\\\\", "\\")
    if len(text) >= 2 and text[0] == "'" and text[-1] == "'":
        return text[1:-1]
    return text


def parse_tags(value: str) -> list[str]:
    """兼容逗号分隔和列表字符串，降低旧内容兼容成本。"""

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
    """同时支持正则与大小写不敏感子串匹配。"""

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
    """头部字段单独参与匹配，避免正文为空时漏掉标题型记忆。"""

    for key in ("project", "title", "summary"):
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
    """标题命中时返回全文，方便快速回看完整结论。"""

    title = header.get("title")
    return isinstance(title, str) and bool(title) and matcher(title)


def read_header(lines: list[str]) -> dict[str, object]:
    """从持久化的 Markdown 内容中恢复头部字段。"""

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
    """过滤出正文命中行，避免头部匹配误入正文片段流程。"""

    out: list[int] = []
    for line_no in sorted(set(line_numbers)):
        if line_no - 1 >= body_start:
            out.append(line_no)
    return out


def header_match_line_numbers(line_numbers: list[int], body_start: int) -> list[int]:
    """过滤出头部命中行，供元数据片段兜底展示。"""

    out: list[int] = []
    for line_no in sorted(set(line_numbers)):
        if line_no - 1 < body_start:
            out.append(line_no)
    return out


def is_heading_line(line: str) -> bool:
    """识别 Markdown 标题，便于按章节返回更完整的上下文。"""

    return HEADING_RE.match(line.strip()) is not None


def heading_level(line: str) -> int:
    """读取标题层级，保证章节截取不会跨越同级块边界。"""

    match = HEADING_RE.match(line.strip())
    if not match:
        return 0
    return len(match.group(1))


def heading_text(line: str) -> str:
    """提取标题文本，用于判断是否是标题自身命中。"""

    match = HEADING_RE.match(line.strip())
    if not match:
        return ""
    return match.group(2).strip()


def section_end_index(body_lines: list[str], heading_idx: int) -> int:
    """找到当前标题块的结束位置，避免截取过多无关内容。"""

    start_level = heading_level(body_lines[heading_idx])
    for idx in range(heading_idx + 1, len(body_lines)):
        if not is_heading_line(body_lines[idx]):
            continue
        if heading_level(body_lines[idx]) <= start_level:
            return idx
    return len(body_lines)


def extract_section(body_lines: list[str], heading_idx: int) -> tuple[str, int, int]:
    """按标题提取完整章节，提升命中结果的可读性。"""

    end_idx = section_end_index(body_lines, heading_idx)
    content = "\n".join(body_lines[heading_idx:end_idx]).strip()
    return content, heading_idx, end_idx


def find_parent_heading_index(body_lines: list[str], line_idx: int) -> int | None:
    """正文命中非标题行时回溯到最近标题，保证语义上下文完整。"""

    for idx in range(line_idx, -1, -1):
        if is_heading_line(body_lines[idx]):
            return idx
    return None


def make_snippet(start_line: int, end_line: int, content: str) -> dict[str, object]:
    """统一片段结构，方便渲染层保持稳定格式。"""

    return {
        "line_range": {"start": start_line, "end": end_line},
        "content": content,
    }


def build_full_file_content(lines: list[str]) -> str:
    """标题命中时返回完整内容，减少重复检索成本。"""

    return "\n".join(lines).strip()


def build_body_section_snippets(
    lines: list[str], body_start: int, match_lines: list[int], matcher
) -> list[dict[str, object]]:
    """正文优先按章节返回片段，让结论和约束一起出现。"""

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
    """无标题上下文时退化为窗口截取，至少保留附近语义。"""

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
    """为仅命中头部字段的记录构建最小可读片段。"""

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
    """按配置解析记忆目录后，从 SQLite 中执行搜索。"""

    args = parse_args()
    queries = parse_queries(args.query)
    if not queries:
        raise SystemExit("--query must contain at least one non-empty keyword")
    location = resolve_memory_location(Path(args.root))
    rebuild_embeddings(location.project_root)
    memory_root = location.memory_root
    project_name = location.project_name
    allow_legacy_blank_project = not location.external_enabled
    provider = create_embedding_provider(location.embedding_config)

    debug_commands: list[str] | None = [] if args.debug else None
    error_keyword_hits = collect_hits(
        "error",
        memory_root,
        project_name,
        allow_legacy_blank_project,
        queries,
        debug_commands,
    )
    summary_keyword_hits = collect_hits(
        "summary",
        memory_root,
        project_name,
        allow_legacy_blank_project,
        queries,
        debug_commands,
    )
    error_semantic_hits = collect_semantic_hits(
        "error",
        memory_root,
        project_name,
        allow_legacy_blank_project,
        queries,
        provider,
        debug_commands,
    )
    summary_semantic_hits = collect_semantic_hits(
        "summary",
        memory_root,
        project_name,
        allow_legacy_blank_project,
        queries,
        provider,
        debug_commands,
    )
    error_hits = merge_hits(error_keyword_hits, error_semantic_hits)
    summary_hits = merge_hits(summary_keyword_hits, summary_semantic_hits)
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
