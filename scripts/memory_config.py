#!/usr/bin/env python3
"""解析记忆脚本使用的外部配置。"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path


CONFIG_PATH = Path.home() / ".config" / "memorymanager" / "config.json"
DEFAULT_STORAGE_ROOT = Path.home() / ".local" / "share" / "memorymanager"
STORAGE_PATH_KEYS = (
    "memory_storage_path",
    "memoryStorePath",
    "storage_path",
    "storagePath",
)
PROJECTS_KEYS = (
    "external_projects",
    "externalProjects",
    "projects",
)


@dataclass(frozen=True)
class MemoryLocation:
    """统一描述当前项目对应的记忆位置与项目标识。"""

    project_root: Path
    project_name: str
    search_root: Path
    memory_root: Path
    external_enabled: bool
    config_path: Path


def resolve_memory_location(project_root: Path) -> MemoryLocation:
    """根据项目根目录与用户配置决定实际记忆目录。"""

    resolved_root = project_root.resolve()
    project_name = sanitize_project_name(resolved_root.name)
    config = load_config(CONFIG_PATH)

    if config and project_in_external_list(config, resolved_root, project_name):
        storage_root = resolve_storage_root(config, CONFIG_PATH.parent)
        if storage_root is not None:
            return MemoryLocation(
                project_root=resolved_root,
                project_name=project_name,
                search_root=storage_root,
                memory_root=storage_root / ".memory",
                external_enabled=True,
                config_path=CONFIG_PATH,
            )

    return MemoryLocation(
        project_root=resolved_root,
        project_name=project_name,
        search_root=resolved_root,
        memory_root=resolved_root / ".memory",
        external_enabled=False,
        config_path=CONFIG_PATH,
    )


def sanitize_project_name(name: str) -> str:
    """保证项目名稳定可用，避免空名或非法片段污染检索维度。"""

    cleaned = name.strip().strip("./")
    return cleaned or "default-project"


def load_config(config_path: Path) -> dict[str, object] | None:
    """读取配置文件，缺失时自动创建默认配置以降低首次使用门槛。"""

    if not config_path.exists():
        default_config = build_default_config()
        if write_default_config(config_path, default_config):
            return default_config
        return None
    try:
        payload = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return payload if isinstance(payload, dict) else None


def build_default_config() -> dict[str, object]:
    """生成默认配置，提前给出统一的外挂记忆落盘位置。"""

    return {
        "memory_storage_path": str(DEFAULT_STORAGE_ROOT),
        "external_projects": [],
    }


def write_default_config(config_path: Path, config: dict[str, object]) -> bool:
    """首次运行自动落默认配置，避免用户必须手写样板文件。"""

    try:
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(
            json.dumps(config, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
    except OSError:
        return False
    return True


def project_in_external_list(
    config: dict[str, object], project_root: Path, project_name: str
) -> bool:
    """兼容项目名、项目根路径和对象写法，尽量减少配置格式耦合。"""

    entries = extract_projects(config)
    project_root_text = str(project_root)
    for entry in entries:
        if isinstance(entry, str):
            normalized = entry.strip()
            if normalized in {project_name, project_root_text}:
                return True
            continue

        if not isinstance(entry, dict):
            continue

        candidate_name = first_text(entry, "name", "project", "project_name")
        if candidate_name and candidate_name == project_name:
            return True

        candidate_path = first_text(entry, "path", "root", "project_root")
        if candidate_path:
            candidate_root = Path(candidate_path).expanduser()
            if not candidate_root.is_absolute():
                candidate_root = (CONFIG_PATH.parent / candidate_root).resolve()
            else:
                candidate_root = candidate_root.resolve()
            if candidate_root == project_root:
                return True
    return False


def extract_projects(config: dict[str, object]) -> list[object]:
    """从多个兼容字段中提取外挂项目列表。"""

    for key in PROJECTS_KEYS:
        value = config.get(key)
        if isinstance(value, list):
            return value
    return []


def resolve_storage_root(config: dict[str, object], base_dir: Path) -> Path | None:
    """解析外挂记忆根目录，支持相对路径配置。"""

    for key in STORAGE_PATH_KEYS:
        value = config.get(key)
        if not isinstance(value, str) or not value.strip():
            continue
        storage_root = Path(value).expanduser()
        if not storage_root.is_absolute():
            storage_root = (base_dir / storage_root).resolve()
        else:
            storage_root = storage_root.resolve()
        return storage_root
    return None


def first_text(payload: dict[str, object], *keys: str) -> str:
    """从候选键中取第一个非空字符串。"""

    for key in keys:
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""
