#!/usr/bin/env python3
"""通过 HTTP 调用记忆服务端的统一脚本入口。"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any
from urllib import error, request


DEFAULT_SERVER_BASE_URL = "http://127.0.0.1:8080"
CONFIG_PATH = Path.home() / ".config" / "memorymanager" / "config.json"


def build_parser() -> argparse.ArgumentParser:
    """统一定义子命令入口，避免多个脚本重复维护参数协议。"""

    parser = argparse.ArgumentParser(prog="memory-manager")
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
    for key in (project_root, str(Path(project_root)), project_name):
        value = projects.get(key)
        if isinstance(value, dict):
            return value
    return {}


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


def resolve_request_target(config: dict[str, Any], root: str) -> tuple[str, str, str]:
    """根据项目配置决定请求地址和远程项目名，保证本地与远程路由一致。"""

    project_root = resolve_project_root(root)
    project_config = lookup_project_config(config, project_root)
    base_url = resolve_server_value(project_config) or resolve_default_server_base_url(config)
    project_alias = resolve_project_alias(project_config)
    return project_root, base_url, project_alias


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


def post_json(base_url: str, path: str, payload: dict[str, Any]) -> dict[str, Any]:
    """统一处理 HTTP 请求和错误解码，避免各子命令重复维护网络细节。"""

    req = request.Request(
        url=f"{base_url}{path}",
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers={"Content-Type": "application/json"},
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
    return payload


def build_request_payload(project_root: str, project_alias: str, **extra: Any) -> dict[str, Any]:
    """只在配置了项目别名时传远程项目名，避免影响未升级的本地调用。"""

    payload: dict[str, Any] = {"project_root": project_root, **extra}
    if project_alias:
        payload["project_name"] = project_alias
    return payload


def run_search(args: argparse.Namespace, project_root: str, base_url: str, project_alias: str) -> int:
    """搜索子命令只整理输入并打印服务端返回结果，保持脚本职责轻量。"""

    queries = parse_queries(args.query)
    if not queries:
        raise SystemExit("--query 至少需要一个非空关键词")
    response = post_json(
        base_url,
        "/api/v1/memories/search",
        build_request_payload(
            project_root,
            project_alias,
            queries=queries,
            debug=bool(args.debug),
        ),
    )
    print(str(response.get("markdown", "")), end="")
    return 0


def run_write(args: argparse.Namespace, project_root: str, base_url: str, project_alias: str) -> int:
    """写入子命令只负责参数兼容和输出结果，把持久化逻辑完全留给服务端。"""

    response = post_json(
        base_url,
        "/api/v1/memories/write",
        build_request_payload(project_root, project_alias, items=parse_write_items(args)),
    )
    print(str(response.get("database_path", "")))
    return 0


def main() -> int:
    """统一脚本入口，根据子命令分发不同 HTTP 调用。"""

    parser = build_parser()
    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        return 1

    project_root, base_url, project_alias = resolve_request_target(load_config(), args.root)
    if args.command == "search":
        return run_search(args, project_root, base_url, project_alias)
    if args.command == "write":
        return run_write(args, project_root, base_url, project_alias)
    parser.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
