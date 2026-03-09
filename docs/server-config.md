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
- `embedding.base_url`：OpenAI 兼容 Embeddings 服务根地址
- `embedding.api_key`：嵌入服务认证令牌
- `embedding.model`：嵌入模型名
- `embedding.timeout_seconds`：嵌入请求超时秒数
- `embedding.semantic_similarity_threshold`：语义命中阈值
- `embedding.semantic_candidate_batch_size`：每批读取的向量候选数
- `embedding.semantic_candidate_max_count`：单次语义搜索最多扫描的候选数
- `embedding.semantic_hit_fetch_limit`：最终回表读取正文的高分候选上限
