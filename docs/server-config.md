# 服务端配置

## 配置文件位置

服务端只读取当前工作目录下的：

```
./config.json
```

## SQLite 配置

```json
{
  "database": {
    "driver": "sqlite",
    "dsn": ""
  }
}
```

当 `dsn` 为空时，默认使用：

```
./.memory/memory.db
```

## PostgreSQL 配置

```json
{
  "database": {
    "driver": "postgresql",
    "dsn": "postgres://user:password@127.0.0.1:5432/hive?sslmode=disable"
  }
}
```

支持的驱动别名：

- `sqlite`
- `postgres`
- `postgresql`

## 配置字段

- `server.base_url`：服务端对外访问地址
- `server.listen_addr`：Gin 实际监听地址
- `auth.jwt_secret`：管理后台 JWT 签名密钥
- `database.driver`：数据库驱动，支持 `sqlite` / `postgresql`
- `database.dsn`：数据库连接串；SQLite 为空时使用默认文件路径
- `search.low_confidence_error_hit_limit`：错误记忆中低于 1 分置信度的最大返回条数
- `search.low_confidence_summary_hit_limit`：总结记忆中低于 1 分置信度的最大返回条数
- `search.keyword.mode`：关键字模式，支持 `like` / `bm25`
- `search.keyword.backend`：关键字后端类型（当前版本主要用于配置预留）
- `search.keyword.bm25_k1`：BM25 `k1` 参数，默认 `1.2`
- `search.keyword.bm25_b`：BM25 `b` 参数，默认 `0.75`
- `search.keyword.fields`：BM25 参与字段，默认 `title/summary/tags/content/project_name`
- `search.keyword.field_weights`：BM25 字段权重映射
- `search.keyword.synonyms.enabled`：是否启用同义词扩展
- `search.keyword.synonyms.groups`：同义词分组，例如 `[ ["error", "故障", "失败"] ]`
- `search.fusion.enabled`：是否启用关键字/语义融合排序
- `search.fusion.formula`：融合公式，当前支持 `weighted_sum`
- `search.fusion.keyword_weight`：关键字权重
- `search.fusion.semantic_weight`：语义权重
- `search.fusion.recency_weight`：时效权重
- `search.fusion.min_semantic_score`：语义分最低有效阈值
- `search.cache.enabled`：是否启用查询缓存
- `search.cache.query_embedding_ttl_seconds`：查询向量缓存 TTL（秒）
- `search.cache.semantic_hits_ttl_seconds`：语义命中缓存 TTL（秒）
- `search.cache.max_entries`：缓存最大条目数
- `search.cache.stats_refresh_interval_seconds`：管理端缓存统计刷新间隔（秒），`0` 表示仅首次加载
- `embedding.base_url`：OpenAI 兼容 Embeddings 服务根地址
- `embedding.api_key`：嵌入服务认证令牌
- `embedding.model`：嵌入模型名
- `embedding.timeout_seconds`：嵌入请求超时秒数
- `embedding.semantic_similarity_threshold`：语义命中阈值
- `embedding.semantic_candidate_batch_size`：每批读取的向量候选数
- `embedding.semantic_candidate_max_count`：单次语义搜索最多扫描的候选数
- `embedding.semantic_hit_fetch_limit`：最终回表读取正文的高分候选上限
- `embedding.semantic_window.mode`：语义候选窗口模式，支持 `static` / `dynamic`
- `embedding.semantic_window.base_max_count`：静态窗口上限，或动态模式下的保底值
- `embedding.semantic_window.dynamic_min_count`：动态窗口最小值
- `embedding.semantic_window.dynamic_max_count`：动态窗口最大值
- `embedding.semantic_window.dynamic_ratio`：动态窗口比例因子
- `embedding.semantic_window.reference_corpus_size`：动态窗口参考语料规模（当前版本为配置预留）
- `embedding.decay.enabled`：是否启用时间衰减融合
- `embedding.decay.age_weight`：时效分权重
- `embedding.decay.semantic_weight`：语义分权重
- `embedding.decay.half_life_days.summary`：总结记忆半衰期（天）
- `embedding.decay.half_life_days.error`：错误记忆半衰期（天）

## 搜索配置示例

```json
{
  "search": {
    "low_confidence_error_hit_limit": 10,
    "low_confidence_summary_hit_limit": 10,
    "keyword": {
      "mode": "bm25",
      "backend": "auto",
      "bm25_k1": 1.2,
      "bm25_b": 0.75,
      "fields": ["title", "summary", "tags", "content", "project_name"],
      "field_weights": {
        "title": 2.0,
        "summary": 1.5,
        "tags": 1.5,
        "content": 1.0,
        "project_name": 0.8
      },
      "synonyms": {
        "enabled": true,
        "groups": [["error", "故障", "失败"]]
      }
    },
    "fusion": {
      "enabled": true,
      "formula": "weighted_sum",
      "keyword_weight": 0.55,
      "semantic_weight": 0.45,
      "recency_weight": 0.1,
      "min_semantic_score": 0.15
    },
    "cache": {
      "enabled": false,
      "query_embedding_ttl_seconds": 600,
      "semantic_hits_ttl_seconds": 120,
      "max_entries": 5000,
      "stats_refresh_interval_seconds": 10
    }
  },
  "embedding": {
    "semantic_window": {
      "mode": "static",
      "base_max_count": 1024,
      "dynamic_min_count": 256,
      "dynamic_max_count": 20000,
      "dynamic_ratio": 0.2,
      "reference_corpus_size": 10000
    },
    "decay": {
      "enabled": true,
      "age_weight": 0.5,
      "semantic_weight": 0.5,
      "half_life_days": {
        "summary": 30,
        "error": 90
      }
    }
  }
}
```
