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

支持的驱动值：

- `sqlite`
- `postgres`
- `postgresql`

## 配置字段

- `server.baseUrl`：服务端对外访问地址
- `server.listenAddr`：Gin 实际监听地址
- `auth.jwtSecret`：管理后台 JWT 签名密钥
- `database.driver`：数据库驱动，支持 `sqlite` / `postgresql`
- `database.dsn`：数据库连接串；SQLite 为空时使用默认文件路径
- `schedule.memoryCleanup.enabled`：是否启用记忆定时清理任务
- `schedule.memoryCleanup.spec`：清理任务 cron 表达式，使用 5 段格式
- `schedule.memoryCleanup.mode`：清理模式，支持 `review` / `auto`
- `schedule.memoryCleanup.dryRun`：是否只生成结果而不真正写入/删除
- `schedule.memoryCleanup.reviewTopN`：单轮最多生成多少条待审核候选
- `schedule.memoryCleanup.protectedTags`：服务启动时补种到数据库的默认保护标签
- `schedule.memoryCleanup.summary.beforeDays`：总结记忆进入候选前至少保留天数
- `schedule.memoryCleanup.summary.batchSize`：总结记忆单轮最多处理条数
- `schedule.memoryCleanup.summary.minRemaining`：总结记忆全局最少保留数
- `schedule.memoryCleanup.summary.minRemainingPerProject`：每个项目的总结记忆最少保留数
- `schedule.memoryCleanup.summary.scoreThreshold`：总结记忆进入候选的最低清理分
- `schedule.memoryCleanup.summary.weights.*`：总结记忆评分权重，支持 `age/useCount/lastUsed/projectPressure`
- `schedule.memoryCleanup.error.beforeDays`：错误记忆进入候选前至少保留天数
- `schedule.memoryCleanup.error.batchSize`：错误记忆单轮最多处理条数
- `schedule.memoryCleanup.error.minRemaining`：错误记忆全局最少保留数
- `schedule.memoryCleanup.error.minRemainingPerProject`：每个项目的错误记忆最少保留数
- `schedule.memoryCleanup.error.scoreThreshold`：错误记忆进入候选的最低清理分
- `schedule.memoryCleanup.error.weights.*`：错误记忆评分权重，支持 `age/useCount/lastUsed/projectPressure`
- `search.lowConfidenceErrorHitLimit`：错误记忆中低于 1 分置信度的最大返回条数
- `search.lowConfidenceSummaryHitLimit`：总结记忆中低于 1 分置信度的最大返回条数
- `search.keyword.mode`：关键字模式，支持 `like` / `bm25`
- `search.keyword.backend`：关键字后端类型（当前版本主要用于配置预留）
- `search.keyword.bm25K1`：BM25 `k1` 参数，默认 `1.2`
- `search.keyword.bm25B`：BM25 `b` 参数，默认 `0.75`
- `search.keyword.fields`：BM25 参与字段，默认 `title/summary/tags/content`
- `search.keyword.fieldWeights`：BM25 字段权重映射
- `search.keyword.synonyms.enabled`：是否启用同义词扩展
- `search.keyword.synonyms.groups`：同义词分组，例如 `[ ["error", "故障", "失败"] ]`
- `search.fusion.enabled`：是否启用关键字/语义融合排序
- `search.fusion.formula`：融合公式，支持 `weighted_sum` / `coverage_discount`，默认 `coverage_discount`
- `search.fusion.keywordWeight`：关键字权重
- `search.fusion.semanticWeight`：语义权重
- `search.fusion.recencyWeight`：时效权重
- `search.fusion.minSemanticScore`：语义分最低有效阈值
- `search.fusion.coverageDiscountBase`：`coverage_discount` 公式的覆盖率折扣基线，默认 `0.85`
- `search.cache.enabled`：是否启用查询缓存
- `search.cache.queryEmbeddingTtlSeconds`：查询向量缓存 TTL（秒）
- `search.cache.semanticHitsTtlSeconds`：语义命中缓存 TTL（秒）
- `search.cache.maxEntries`：缓存最大条目数
- `search.cache.statsRefreshIntervalSeconds`：管理端缓存统计刷新间隔（秒），`0` 表示仅首次加载
- `search.searchStageConcurrency`：搜索编排阶段最大并发数
- `embedding.baseUrl`：OpenAI 兼容 Embeddings 服务根地址
- `embedding.apiKey`：嵌入服务认证令牌
- `embedding.model`：嵌入模型名
- `embedding.timeoutSeconds`：嵌入请求超时秒数
- `embedding.semanticSimilarityThreshold`：语义命中阈值
- `embedding.semanticCandidateBatchSize`：每批读取的向量候选数
- `embedding.semanticCandidateMaxCount`：单次语义搜索最多扫描的候选数
- `embedding.semanticHitFetchLimit`：最终回表读取正文的高分候选上限
- `embedding.semanticSearchConcurrency`：单次语义搜索内多查询向量的数据库检索最大并发数
- `embedding.semanticWindow.mode`：语义候选窗口模式，支持 `static` / `dynamic`
- `embedding.semanticWindow.baseMaxCount`：静态窗口上限，或动态模式下的保底值
- `embedding.semanticWindow.dynamicMinCount`：动态窗口最小值
- `embedding.semanticWindow.dynamicMaxCount`：动态窗口最大值
- `embedding.semanticWindow.dynamicRatio`：动态窗口比例因子
- `embedding.semanticWindow.referenceCorpusSize`：动态窗口参考语料规模（当前版本为配置预留）
- `embedding.decay.enabled`：是否启用时间衰减融合
- `embedding.decay.ageWeight`：时效分权重
- `embedding.decay.semanticWeight`：语义分权重
- `embedding.decay.halfLifeDays.summary`：总结记忆半衰期（天）
- `embedding.decay.halfLifeDays.error`：错误记忆半衰期（天）

## 搜索配置示例

```json
{
  "schedule": {
    "memoryCleanup": {
      "enabled": false,
      "spec": "0 3 * * *",
      "mode": "review",
      "dryRun": false,
      "reviewTopN": 200,
      "protectedTags": ["核心故障", "架构决策"],
      "summary": {
        "beforeDays": 30,
        "batchSize": 100,
        "minRemaining": 500,
        "minRemainingPerProject": 20,
        "scoreThreshold": 0.65,
        "weights": {
          "age": 0.3,
          "useCount": 0.35,
          "lastUsed": 0.25,
          "projectPressure": 0.1
        }
      },
      "error": {
        "beforeDays": 120,
        "batchSize": 30,
        "minRemaining": 1000,
        "minRemainingPerProject": 50,
        "scoreThreshold": 0.8,
        "weights": {
          "age": 0.2,
          "useCount": 0.25,
          "lastUsed": 0.35,
          "projectPressure": 0.2
        }
      }
    }
  },
  "search": {
    "lowConfidenceErrorHitLimit": 10,
    "lowConfidenceSummaryHitLimit": 10,
    "searchStageConcurrency": 2,
    "keyword": {
      "mode": "bm25",
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
        "groups": [["error", "故障", "失败"]]
      }
    },
    "fusion": {
      "enabled": true,
      "formula": "coverage_discount",
      "keywordWeight": 0.55,
      "semanticWeight": 0.45,
      "recencyWeight": 0.1,
      "minSemanticScore": 0.15,
      "coverageDiscountBase": 0.85
    },
    "cache": {
      "enabled": false,
      "queryEmbeddingTtlSeconds": 600,
      "semanticHitsTtlSeconds": 120,
      "maxEntries": 5000,
      "statsRefreshIntervalSeconds": 10
    }
  },
  "embedding": {
    "semanticSearchConcurrency": 4,
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

## 清理治理说明

- 搜索真正返回给调用方的命中结果时，服务端会自动累计 `use_count` 并更新 `last_used_at`
- 保护标签以数据库表 `memory_protected_tags` 为运行时真值，配置里的 `protectedTags` 只用于首次补种
- `review` 模式下 cron 只生成待审核清单；`auto` 模式下会直接执行删除
- 审核和执行删除都通过管理端接口完成，避免定时任务误删未确认候选
