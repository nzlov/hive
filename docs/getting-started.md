# 快速开始

## 准备服务端配置

在服务端启动目录创建 `config.json`：

```json
{
  "server": {
    "baseUrl": "http://127.0.0.1:8080",
    "listenAddr": ":8080"
  },
  "auth": {
    "jwtSecret": "please-change-this-secret"
  },
  "database": {
    "driver": "sqlite",
    "dsn": ""
  },
  "search": {
    "lowConfidenceErrorHitLimit": 10,
    "lowConfidenceSummaryHitLimit": 10,
    "keyword": {
      "mode": "like",
      "backend": "auto",
      "bm25K1": 1.2,
      "bm25B": 0.75,
      "fields": ["title", "summary", "tags", "content"],
      "fieldWeights": {
        "title": 2.0,
        "summary": 1.5,
        "tags": 1.5,
        "content": 1.0
      },
      "synonyms": {
        "enabled": true,
        "groups": []
      }
    },
    "fusion": {
      "enabled": true,
      "formula": "weighted_sum",
      "keywordWeight": 0.55,
      "semanticWeight": 0.45,
      "recencyWeight": 0.1,
      "minSemanticScore": 0.15
    },
    "cache": {
      "enabled": false,
      "queryEmbeddingTtlSeconds": 600,
      "semanticHitsTtlSeconds": 120,
      "maxEntries": 5000
    }
  },
  "embedding": {
    "baseUrl": "",
    "apiKey": "",
    "model": "",
    "timeoutSeconds": 30,
    "semanticSimilarityThreshold": 0.15,
    "semanticCandidateBatchSize": 256,
    "semanticCandidateMaxCount": 1024,
    "semanticHitFetchLimit": 64,
    "semanticWindow": {
      "mode": "static",
      "baseMaxCount": 1024,
      "dynamicMinCount": 256,
      "dynamicMaxCount": 20000,
      "dynamicRatio": 0.2,
      "referenceCorpusSize": 10000
    },
    "decay": {
      "enabled": true,
      "ageWeight": 0.5,
      "semanticWeight": 0.5,
      "halfLifeDays": {
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
