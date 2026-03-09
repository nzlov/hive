# 快速开始

## 准备服务端配置

在服务端启动目录创建 `config.json`：

```json
{
  "server": {
    "base_url": "http://127.0.0.1:8080",
    "listen_addr": ":8080"
  },
  "auth": {
    "jwt_secret": "please-change-this-secret"
  },
  "database": {
    "driver": "sqlite",
    "dsn": ""
  },
  "search": {
    "low_confidence_error_hit_limit": 10,
    "low_confidence_summary_hit_limit": 10,
    "keyword": {
      "mode": "like",
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
        "groups": []
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
      "max_entries": 5000
    }
  },
  "embedding": {
    "base_url": "",
    "api_key": "",
    "model": "",
    "timeout_seconds": 30,
    "semantic_similarity_threshold": 0.15,
    "semantic_candidate_batch_size": 256,
    "semantic_candidate_max_count": 1024,
    "semantic_hit_fetch_limit": 64,
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

说明：

- `database.driver` 为空或缺失时，默认回落到 `SQLite`
- `SQLite` 默认使用 `./.memory/memory.db`
- `PostgreSQL` 需要显式设置 `database.driver=postgresql` 和 `database.dsn`

## 启动服务端

```bash
go run ./cmd/hive-server
```

或使用：

```bash
make dev-server
```

首次启动如果数据库里没有任何用户，服务端会自动创建默认管理员：

- 用户名：`admin`
- 真实名称：`系统管理员`
- `userid`：自动生成 `UUID`
- 密码：随机生成，并打印在启动日志中
- `apitoken`：自动生成，可在管理后台查看

## 健康检查

```bash
curl http://127.0.0.1:8080/healthz
```

返回：

```json
{"status":"ok"}
```

## 客户端搜索示例

```bash
python3 hive/scripts/main.py search --root . --query "向量重建"
```

## 客户端写入示例

```bash
python3 hive/scripts/main.py write \
  --root . \
  --items-json '[
    {
      "type": "summary",
      "title": "Hive 接入方式",
      "tags": ["hive", "接入"],
      "summary": "说明如何启动服务和调用客户端。",
      "context": "## Summary\n\n- 详情: Hive 通过 Python 脚本调用 Go 服务端。"
    }
  ]'
```

## 启动前端开发环境

```bash
make dev-ui
```

前端默认监听 `http://127.0.0.1:5173`，并通过 Vite 代理转发：

- `/api/*` -> `http://127.0.0.1:8080`
- `/tokenapi/*` -> `http://127.0.0.1:8080`
