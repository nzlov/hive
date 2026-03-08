#!/usr/bin/env python3
"""提供嵌入能力抽象，兼容关闭模式与 OpenAI 兼容接口。"""

from __future__ import annotations

import json
import math
from dataclasses import dataclass
from typing import Protocol
from urllib import error, request

from memory_config import EmbeddingConfig


class EmbeddingProvider(Protocol):
    """统一嵌入接口，避免写入和检索逻辑直接依赖具体服务商。"""

    @property
    def enabled(self) -> bool:
        """声明当前 provider 是否可用，方便上层快速短路。"""

    @property
    def model_name(self) -> str:
        """返回当前使用的模型名，供数据库记录和模型切换检测复用。"""

    def embed_texts(self, texts: list[str]) -> list[list[float]]:
        """按输入顺序返回向量，保证批量写入和重建阶段可稳定对齐。"""


@dataclass(frozen=True)
class DisabledEmbeddingProvider:
    """保留无嵌入模式，确保现有关键字检索流程继续工作。"""

    @property
    def enabled(self) -> bool:
        return False

    @property
    def model_name(self) -> str:
        return ""

    def embed_texts(self, texts: list[str]) -> list[list[float]]:
        return []


@dataclass(frozen=True)
class OpenAIEmbeddingProvider:
    """复用 OpenAI Embeddings API 结构，降低接入兼容服务商的成本。"""

    config: EmbeddingConfig

    @property
    def enabled(self) -> bool:
        return True

    @property
    def model_name(self) -> str:
        return self.config.model

    def embed_texts(self, texts: list[str]) -> list[list[float]]:
        """批量调用 Embeddings 接口，保证重建和查询走同一条协议链路。"""

        if not texts:
            return []
        payload = json.dumps(
            {
                "model": self.config.model,
                "input": texts,
            }
        ).encode("utf-8")
        req = request.Request(
            url=f"{self.config.base_url}/embeddings",
            data=payload,
            headers={
                "Authorization": f"Bearer {self.config.api_key}",
                "Content-Type": "application/json",
            },
            method="POST",
        )
        try:
            with request.urlopen(req, timeout=self.config.timeout_seconds) as response:
                raw = response.read().decode("utf-8")
        except error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"嵌入请求失败: HTTP {exc.code} {detail}") from exc
        except error.URLError as exc:
            raise RuntimeError(f"嵌入请求失败: {exc.reason}") from exc

        try:
            body = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError("嵌入请求返回了无法解析的 JSON") from exc

        data = body.get("data")
        if not isinstance(data, list):
            raise RuntimeError("嵌入响应缺少 data 字段")
        vectors: list[list[float]] = []
        for item in data:
            if not isinstance(item, dict):
                raise RuntimeError("嵌入响应数据格式不正确")
            embedding = item.get("embedding")
            if not isinstance(embedding, list):
                raise RuntimeError("嵌入响应缺少 embedding 字段")
            try:
                vectors.append([float(value) for value in embedding])
            except (TypeError, ValueError) as exc:
                raise RuntimeError("嵌入响应包含非法向量值") from exc
        if len(vectors) != len(texts):
            raise RuntimeError("嵌入响应数量与请求数量不一致")
        return vectors


def create_embedding_provider(config: EmbeddingConfig | None) -> EmbeddingProvider:
    """通过工厂函数隐藏实现细节，让调用方只依赖统一接口。"""

    if config is None:
        return DisabledEmbeddingProvider()
    return OpenAIEmbeddingProvider(config)


def build_memory_embedding_text(row) -> str:
    """拼接对检索最重要的字段，避免向量仅看到正文而忽略标题和摘要。"""

    parts = [
        f"project: {str(row['project_name'] or '').strip()}",
        f"type: {str(row['type'] or '').strip()}",
        f"title: {str(row['title'] or '').strip()}",
        f"summary: {str(row['summary'] or '').strip()}",
        f"tags: {str(row['tags'] or '').strip()}",
        str(row["content"] or "").strip(),
    ]
    return "\n".join(part for part in parts if part)


def cosine_similarity(left: list[float], right: list[float]) -> float:
    """用余弦相似度衡量语义接近度，避免向量长度差异带来偏置。"""

    if not left or not right or len(left) != len(right):
        return 0.0
    numerator = 0.0
    left_norm = 0.0
    right_norm = 0.0
    for l_value, r_value in zip(left, right):
        numerator += l_value * r_value
        left_norm += l_value * l_value
        right_norm += r_value * r_value
    if left_norm <= 0.0 or right_norm <= 0.0:
        return 0.0
    return numerator / (math.sqrt(left_norm) * math.sqrt(right_norm))
