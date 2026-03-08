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
    """优先使用项目内 `.memory`，缺失时再回退到默认外挂目录。"""

    resolved_root = project_root.resolve()
    project_name = sanitize_project_name(resolved_root.name)
    config = load_config(CONFIG_PATH)
    local_memory_root = resolved_root / ".memory"

    if local_memory_root.is_dir():
        return MemoryLocation(
            project_root=resolved_root,
            project_name=project_name,
            search_root=resolved_root,
            memory_root=local_memory_root,
            external_enabled=False,
            config_path=CONFIG_PATH,
        )

    storage_root = resolve_storage_root(config or build_default_config(), CONFIG_PATH.parent)
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
