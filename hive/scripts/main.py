#!/usr/bin/env python3
"""通过 HTTP 调用记忆服务端的统一脚本入口。"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit
from urllib import error, request


DEFAULT_SERVER_BASE_URL = "http://127.0.0.1:8080"
CONFIG_PATH = Path.home() / ".config" / "hive" / "config.json"


def build_parser() -> argparse.ArgumentParser:
    """统一定义子命令入口，避免多个脚本重复维护参数协议。"""

    parser = argparse.ArgumentParser(prog="hive")
    subparsers = parser.add_subparsers(dest="command")

    search_parser = subparsers.add_parser("search")
    search_parser.add_argument("--root", default=".", help="项目根目录")
    search_parser.add_argument("--query", nargs="+", required=True, help="搜索关键词或 JSON 数组")
    search_parser.add_argument("-debug", "--debug", action="store_true", help="输出调试信息")

    write_parser = subparsers.add_parser("write", help="批量写入多条记忆")
    write_parser.add_argument("--root", default=".", help="项目根目录")
    write_parser.add_argument(
        "--items-json",
        required=True,
        help="记忆对象 JSON 数组，单次可混合写入多条 summary/error",
    )

    return parser


def load_config() -> dict[str, Any]:
    """脚本只读取本地配置，缺失时回退默认值避免阻塞调用。"""

    if not CONFIG_PATH.exists():
        return {}
    try:
        payload = json.loads(CONFIG_PATH.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return payload if isinstance(payload, dict) else {}


def resolve_default_server_base_url(config: dict[str, Any]) -> str:
    """优先读取默认服务地址配置，避免脚本和服务端地址硬编码漂移。"""

    for key in ("default_server_base_url", "defaultServerBaseUrl"):
        value = config.get(key)
        if isinstance(value, str) and value.strip():
            return value.rstrip("/")

    server = config.get("server")
    if isinstance(server, dict):
        for key in ("base_url", "baseUrl", "url", "address"):
            value = server.get(key)
            if isinstance(value, str) and value.strip():
                return value.rstrip("/")
    for key in ("server_url", "serverUrl", "service_url", "serviceUrl"):
        value = config.get(key)
        if isinstance(value, str) and value.strip():
            return value.rstrip("/")
    return DEFAULT_SERVER_BASE_URL


def resolve_project_root(root: str) -> str:
    """统一把项目根目录转成绝对路径，避免服务端按不同相对路径落不同项目维度。"""

    return str(Path(root).resolve())


def lookup_project_config(config: dict[str, Any], project_root: str) -> dict[str, Any]:
    """按项目根目录匹配项目配置，优先保证同一项目命中稳定配置。"""

    projects = config.get("projects")
    if not isinstance(projects, dict):
        return {}

    project_name = Path(project_root).name
    remote_project_name = resolve_git_remote_project_name(project_root)
    for key in (project_root, str(Path(project_root)), remote_project_name, project_name):
        if not key:
            continue
        value = projects.get(key)
        if isinstance(value, dict):
            return value
    return {}


def normalize_git_remote(raw_remote: str) -> str:
    """统一裁剪 Git 仓库地址，只保留稳定仓库标识，避免协议差异影响项目隔离。"""

    text = raw_remote.strip()
    if not text:
        return ""
    if "://" in text:
        parsed = urlsplit(text)
        host = parsed.netloc
        path = parsed.path.lstrip("/")
        if "@" in host:
            host = host.split("@", 1)[1]
        return "/".join(part for part in (host, path) if part).strip("/")
    if "@" in text and ":" in text:
        text = text.split("@", 1)[1]
        host, path = text.split(":", 1)
        return f"{host}/{path.lstrip('/')}".strip("/")
    return re.sub(r"^[A-Za-z0-9_.-]+@", "", text).strip("/")


def resolve_git_remote_project_name(project_root: str) -> str:
    """优先从 Git 远端推导项目名，保证同一仓库在不同本地路径下仍命中同一记忆空间。"""

    remote = run_git_command(project_root, ["remote", "get-url", "origin"])
    if not remote:
        remotes = run_git_command(project_root, ["remote"])
        first_remote = next((line.strip() for line in remotes.splitlines() if line.strip()), "")
        if first_remote:
            remote = run_git_command(project_root, ["remote", "get-url", first_remote])
    return normalize_git_remote(remote)


def resolve_default_project_name(project_root: str) -> str:
    """统一推导项目名，优先使用 Git 仓库地址，其次回退到目录名。"""

    return resolve_git_remote_project_name(project_root) or Path(project_root).name


def resolve_server_value(payload: dict[str, Any]) -> str:
    """统一兼容多种服务地址字段命名，减少配置迁移成本。"""

    server = payload.get("server")
    if isinstance(server, dict):
        for key in ("base_url", "baseUrl", "url", "address"):
            value = server.get(key)
            if isinstance(value, str) and value.strip():
                return value.rstrip("/")

    for key in (
        "server_url",
        "serverUrl",
        "service_url",
        "serviceUrl",
        "base_url",
        "baseUrl",
        "url",
        "address",
    ):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.rstrip("/")
    return ""


def resolve_project_alias(project_config: dict[str, Any]) -> str:
    """兼容项目别名的不同键名，避免配置字段调整影响调用链路。"""

    for key in ("alias", "project_alias", "projectAlias", "project_name", "projectName", "name"):
        value = project_config.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def resolve_api_token(payload: dict[str, Any]) -> str:
    """统一兼容多种 API Token 字段命名，避免客户端配置升级时请求链路失效。"""

    auth = payload.get("auth")
    if isinstance(auth, dict):
        for key in ("api_token", "apiToken", "apitoken", "token"):
            value = auth.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
    for key in ("api_token", "apiToken", "apitoken", "token"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def resolve_request_target(config: dict[str, Any], root: str) -> tuple[str, str, str, str]:
    """根据项目配置和本地仓库信息决定请求地址、项目名与 API Token，保证单库隔离稳定。"""

    project_root = resolve_project_root(root)
    project_config = lookup_project_config(config, project_root)
    base_url = resolve_server_value(project_config) or resolve_default_server_base_url(config)
    project_name = resolve_project_alias(project_config) or resolve_default_project_name(project_root)
    api_token = resolve_api_token(project_config) or resolve_api_token(config)
    return project_root, base_url, project_name, api_token


def parse_queries(raw_queries: list[str]) -> list[str]:
    """兼容 JSON 数组和多参数写法，减少调用方改造成本。"""

    if not raw_queries:
        return []
    if len(raw_queries) == 1:
        text = raw_queries[0].strip()
        if text.startswith("[") and text.endswith("]"):
            try:
                payload = json.loads(text)
            except json.JSONDecodeError:
                payload = None
            if isinstance(payload, list):
                return [str(item).strip() for item in payload if str(item).strip()]
    return [query.strip() for query in raw_queries if query.strip()]


def split_tags(raw_tags: str) -> list[str]:
    """统一清洗逗号分隔标签，避免空标签进入请求体。"""

    return [part.strip() for part in raw_tags.split(",") if part.strip()]


def normalize_tags(raw_tags: object) -> list[str]:
    """兼容字符串和数组标签写法，减少批量写入时的额外转换逻辑。"""

    if isinstance(raw_tags, str):
        return split_tags(raw_tags)
    if not isinstance(raw_tags, list):
        return []
    return [str(item).strip() for item in raw_tags if str(item).strip()]


def parse_write_items(args: argparse.Namespace) -> list[dict[str, Any]]:
    """写入只接受记忆数组，避免继续维护单条与批量两套协议。"""

    if not args.items_json.strip():
        raise SystemExit("--items-json 不能为空，且必须是记忆对象数组")

    try:
        payload = json.loads(args.items_json)
    except json.JSONDecodeError as exc:
        raise SystemExit("--items-json 必须是合法 JSON") from exc

    if not isinstance(payload, list):
        raise SystemExit("--items-json 必须是对象数组")

    items: list[dict[str, Any]] = []
    for index, item in enumerate(payload, start=1):
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
            {
                "type": mem_type,
                "title": title,
                "tags": normalize_tags(item.get("tags", [])),
                "summary": str(item.get("summary", "")).strip(),
                "context": context,
            }
        )
    if not items:
        raise SystemExit("--items-json 不能为空数组")
    return items


def post_json(base_url: str, path: str, payload: dict[str, Any], api_token: str = "") -> dict[str, Any]:
    """统一处理 HTTP 请求和错误解码，避免各子命令重复维护网络细节。"""

    headers = {"Content-Type": "application/json"}
    if api_token.strip():
        headers["X-API-Token"] = api_token.strip()

    req = request.Request(
        url=f"{base_url}{path}",
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with request.urlopen(req, timeout=30) as response:
            raw_body = response.read().decode("utf-8")
    except error.HTTPError as exc:
        raw_body = exc.read().decode("utf-8", errors="replace")
        try:
            payload = json.loads(raw_body)
        except json.JSONDecodeError:
            payload = {}
        message = payload.get("error") if isinstance(payload, dict) else ""
        raise SystemExit(str(message).strip() or raw_body.strip() or f"HTTP {exc.code}") from exc
    except error.URLError as exc:
        raise SystemExit(f"服务端请求失败: {exc.reason}") from exc

    try:
        payload = json.loads(raw_body)
    except json.JSONDecodeError as exc:
        raise SystemExit("服务端返回了无法解析的 JSON") from exc
    if not isinstance(payload, dict):
        raise SystemExit("服务端返回格式不正确")
    message = payload.get("error")
    if isinstance(message, str) and message.strip():
        raise SystemExit(message.strip())
    return payload


def build_request_payload(project_name: str, **extra: Any) -> dict[str, Any]:
    """统一只向服务端传项目名，避免数据库隔离规则再依赖本地路径。"""

    return {"project_name": project_name, **extra}


def run_git_command(project_root: str, args: list[str]) -> str:
    """统一执行 Git 命令并吞掉环境差异错误，避免脚本因为非仓库目录直接失败。"""

    try:
        completed = subprocess.run(
            ["git", *args],
            cwd=project_root,
            check=True,
            capture_output=True,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError):
        return ""
    return completed.stdout.strip()


def resolve_current_git_branch(project_root: str) -> str:
    """优先读取当前检出分支，便于只保留已经进入当前分支历史的记忆。"""

    branch = run_git_command(project_root, ["branch", "--show-current"])
    if branch:
        return branch
    fallback = run_git_command(project_root, ["rev-parse", "--abbrev-ref", "HEAD"])
    return "" if fallback == "HEAD" else fallback


def normalize_git_branch(git_branch: str) -> str:
    """统一裁剪分支名，避免头部或 Git 输出中的空白影响匹配。"""

    return git_branch.strip()


def branch_ref_candidates(git_branch: str) -> list[str]:
    """兼容本地和远程引用名，尽量提高已合并判断的命中率。"""

    normalized = normalize_git_branch(git_branch)
    if not normalized:
        return []
    candidates = [normalized]
    if not normalized.startswith("refs/"):
        candidates.append(f"refs/heads/{normalized}")
        candidates.append(f"refs/remotes/origin/{normalized}")
        candidates.append(f"origin/{normalized}")
    deduped: list[str] = []
    for candidate in candidates:
        if candidate not in deduped:
            deduped.append(candidate)
    return deduped


def branch_exists(project_root: str, git_branch: str) -> bool:
    """先确认分支引用存在，再做祖先判断，避免缺失引用时误报已合并。"""

    return bool(run_git_command(project_root, ["rev-parse", "--verify", f"{git_branch}^{{commit}}"]))


def is_branch_reachable(project_root: str, git_branch: str) -> bool:
    """通过祖先关系判断目标分支提交是否已经进入当前 HEAD。"""

    try:
        subprocess.run(
            ["git", "merge-base", "--is-ancestor", git_branch, "HEAD"],
            cwd=project_root,
            check=True,
            capture_output=True,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError):
        return False
    return True


def should_keep_hit(hit: dict[str, Any], project_root: str, current_branch: str, branch_cache: dict[str, bool]) -> bool:
    """逐条判断分支记忆是否已经被当前分支吸收，避免把未合并结论提前暴露出来。"""

    memory_branch = normalize_git_branch(str(hit.get("git_branch", "")))
    if not memory_branch or not current_branch:
        return True
    if memory_branch == current_branch:
        return True
    cached = branch_cache.get(memory_branch)
    if cached is not None:
        return cached
    for candidate in branch_ref_candidates(memory_branch):
        if not branch_exists(project_root, candidate):
            continue
        merged = is_branch_reachable(project_root, candidate)
        branch_cache[memory_branch] = merged
        return merged
    branch_cache[memory_branch] = False
    return False


def filter_hits_by_branch(hits: list[dict[str, Any]], project_root: str, current_branch: str) -> list[dict[str, Any]]:
    """按当前项目分支筛掉未合并的分支记忆，保证搜索结果和代码历史一致。"""

    branch_cache: dict[str, bool] = {}
    return [hit for hit in hits if should_keep_hit(hit, project_root, current_branch, branch_cache)]


def markdown_fence_for(text: str) -> str:
    """根据正文内容选择围栏长度，避免记忆正文内已有代码块时被截断。"""

    return "````" if "```" in text else "```"


def render_hit(hit: dict[str, Any], index: int) -> list[str]:
    """统一渲染单条命中，保证脚本筛选后仍保持服务端原有展示结构。"""

    lines = [f"### Record {index}", f"- source: {hit.get('source', '')}", f"- path: {hit.get('path', '')}"]
    project_name = str(hit.get("project_name", "")).strip()
    if project_name:
        lines.append(f"- project: {project_name}")
    git_branch = normalize_git_branch(str(hit.get("git_branch", "")))
    if git_branch:
        lines.append(f"- git_branch: {git_branch}")
    lines.append(f"- timestamp: {hit.get('timestamp', '')}")
    lines.append(f"- confidence: {float(hit.get('confidence', 0.0)):.3f}")
    file_content = str(hit.get("file_content", "")).strip()
    if file_content:
        fence = markdown_fence_for(file_content)
        lines.extend(["- file_content:", f"  {fence}markdown"])
        lines.extend([f"  {line}" for line in file_content.splitlines()])
        lines.append(f"  {fence}")
    snippets = hit.get("snippets")
    if isinstance(snippets, list) and snippets:
        lines.append("- snippets:")
        for snippet in snippets:
            if not isinstance(snippet, dict):
                continue
            content = str(snippet.get("content", "")).strip()
            fence = markdown_fence_for(content)
            lines.extend(
                [
                    f"  - line_range: {int(snippet.get('start', 0))}-{int(snippet.get('end', 0))}",
                    "    content:",
                    f"    {fence}markdown",
                ]
            )
            lines.extend([f"    {line}" for line in content.splitlines()])
            lines.append(f"    {fence}")
    return lines


def render_hits_section(title: str, hits: list[dict[str, Any]]) -> str:
    """统一渲染分类结果，让筛选后的空结果也能稳定展示。"""

    lines = [f"## {title} ({len(hits)})"]
    if not hits:
        lines.append("- (none)")
        return "\n".join(lines)
    for index, hit in enumerate(hits, start=1):
        lines.extend(render_hit(hit, index))
        if index < len(hits):
            lines.append("---")
    return "\n".join(lines)


def render_search_markdown(response: dict[str, Any], error_hits: list[dict[str, Any]], summary_hits: list[dict[str, Any]]) -> str:
    """脚本在本地重建 Markdown，保证分支筛选后输出仍与旧格式兼容。"""

    lines = [
        "# Hive Search Result",
        f"- query: {response.get('query', '')}",
        f"- project_name: {response.get('project_name', '')}",
    ]
    debug_commands = response.get("debug_commands")
    if isinstance(debug_commands, list) and debug_commands:
        lines.extend(["- debug: true", "", "## Debug Commands"])
        lines.extend([f"- `{str(command)}`" for command in debug_commands])
    lines.extend(["", render_hits_section("Error Hits", error_hits), "", render_hits_section("Summary Hits", summary_hits), ""])
    return "\n".join(lines)


def run_search(args: argparse.Namespace, project_root: str, base_url: str, project_name: str, api_token: str) -> int:
    """搜索子命令只整理输入并打印服务端返回结果，保持脚本职责轻量。"""

    queries = parse_queries(args.query)
    if not queries:
        raise SystemExit("--query 至少需要一个非空关键词")
    if not api_token.strip():
        raise SystemExit("缺少 API Token，请在 ~/.config/hive/config.json 中配置 api_token")
    response = post_json(
        base_url,
        "/tokenapi/v1/memories/search",
        build_request_payload(
            project_name,
            queries=queries,
            debug=bool(args.debug),
        ),
        api_token=api_token,
    )
    current_branch = resolve_current_git_branch(project_root)
    error_hits = filter_hits_by_branch(
        response.get("error_hits", []) if isinstance(response.get("error_hits"), list) else [],
        project_root,
        current_branch,
    )
    summary_hits = filter_hits_by_branch(
        response.get("summary_hits", []) if isinstance(response.get("summary_hits"), list) else [],
        project_root,
        current_branch,
    )
    print(render_search_markdown(response, error_hits, summary_hits), end="")
    return 0


def run_write(args: argparse.Namespace, project_root: str, base_url: str, project_name: str, api_token: str) -> int:
    """写入子命令只负责参数兼容和输出结果，把持久化逻辑完全留给服务端。"""

    current_branch = resolve_current_git_branch(project_root)
    if not api_token.strip():
        raise SystemExit("缺少 API Token，请在 ~/.config/hive/config.json 中配置 api_token")
    post_json(
        base_url,
        "/tokenapi/v1/memories/write",
        build_request_payload(project_name, git_branch=current_branch, items=parse_write_items(args)),
        api_token=api_token,
    )
    return 0


def main() -> int:
    """统一脚本入口，根据子命令分发不同 HTTP 调用。"""

    parser = build_parser()
    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        return 1

    project_root, base_url, project_name, api_token = resolve_request_target(load_config(), args.root)
    if args.command == "search":
        return run_search(args, project_root, base_url, project_name, api_token)
    if args.command == "write":
        return run_write(args, project_root, base_url, project_name, api_token)
    parser.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
