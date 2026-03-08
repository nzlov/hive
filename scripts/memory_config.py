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
EMBEDDING_SECTION_KEYS = ("embedding", "embeddings")
EMBEDDING_BASE_URL_KEYS = ("base_url", "baseUrl", "url", "endpoint")
EMBEDDING_API_KEY_KEYS = ("api_key", "apiKey")
EMBEDDING_MODEL_KEYS = ("model", "embedding_model", "embeddingModel")
EMBEDDING_TIMEOUT_KEYS = ("timeout_seconds", "timeoutSeconds")


@dataclass(frozen=True)
class EmbeddingConfig:
    """统一描述嵌入服务配置，避免脚本各自解析字段。"""

    base_url: str
    api_key: str
    model: str
    timeout_seconds: float


@dataclass(frozen=True)
class MemoryLocation:
    """统一描述当前项目对应的记忆位置与项目标识。"""

    project_root: Path
    project_name: str
    search_root: Path
    memory_root: Path
    external_enabled: bool
    config_path: Path
    embedding_config: EmbeddingConfig | None


def resolve_memory_location(project_root: Path) -> MemoryLocation:
    """优先使用项目内 `.memory`，缺失时再回退到默认外挂目录。"""

    resolved_root = project_root.resolve()
    project_name = sanitize_project_name(resolved_root.name)
    config = load_config(CONFIG_PATH)
    embedding_config = resolve_embedding_config(config or {})
    local_memory_root = resolved_root / ".memory"

    if local_memory_root.is_dir():
        return MemoryLocation(
            project_root=resolved_root,
            project_name=project_name,
            search_root=resolved_root,
            memory_root=local_memory_root,
            external_enabled=False,
            config_path=CONFIG_PATH,
            embedding_config=embedding_config,
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
            embedding_config=embedding_config,
        )

    return MemoryLocation(
        project_root=resolved_root,
        project_name=project_name,
        search_root=resolved_root,
        memory_root=resolved_root / ".memory",
        external_enabled=False,
        config_path=CONFIG_PATH,
        embedding_config=embedding_config,
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
        "embedding": {
            "base_url": "",
            "api_key": "",
            "model": "",
            "timeout_seconds": 30,
        },
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


def resolve_embedding_config(config: dict[str, object]) -> EmbeddingConfig | None:
    """兼容多种字段命名，提取可用的嵌入服务配置。"""

    section = find_embedding_section(config)
    if section is None:
        return None
    base_url = pick_string(section, EMBEDDING_BASE_URL_KEYS)
    api_key = pick_string(section, EMBEDDING_API_KEY_KEYS)
    model = pick_string(section, EMBEDDING_MODEL_KEYS)
    if not base_url or not api_key or not model:
        return None
    timeout_seconds = pick_float(section, EMBEDDING_TIMEOUT_KEYS, default=30.0)
    return EmbeddingConfig(
        base_url=base_url.rstrip("/"),
        api_key=api_key,
        model=model,
        timeout_seconds=max(1.0, timeout_seconds),
    )


def find_embedding_section(config: dict[str, object]) -> dict[str, object] | None:
    """优先读取嵌套配置，必要时兼容平铺字段，减少升级摩擦。"""

    for key in EMBEDDING_SECTION_KEYS:
        value = config.get(key)
        if isinstance(value, dict):
            return value
    return config if isinstance(config, dict) else None


def pick_string(payload: dict[str, object], keys: tuple[str, ...]) -> str:
    """从候选字段里取第一个非空字符串，避免脚本端写重复解析。"""

    for key in keys:
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def pick_float(payload: dict[str, object], keys: tuple[str, ...], default: float) -> float:
    """对超时时间做宽松解析，避免配置类型差异导致功能失效。"""

    for key in keys:
        value = payload.get(key)
        if isinstance(value, (int, float)):
            return float(value)
        if isinstance(value, str) and value.strip():
            try:
                return float(value)
            except ValueError:
                continue
    return default
