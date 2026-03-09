# 嵌入与语义检索

当 `embedding.base_url` 和 `embedding.model` 配置完整时，会启用语义检索；`api_key` 可为空，以兼容本地无鉴权服务。

## 可选调优项

- `embedding.semantic_similarity_threshold` 默认 `0.15`
- `embedding.semantic_candidate_batch_size` 默认 `256`
- `embedding.semantic_candidate_max_count` 默认 `1024`
- `embedding.semantic_hit_fetch_limit` 默认 `64`

## 启用后行为

- 写入时同步写入向量
- 搜索时在关键字命中外追加语义召回
- 服务启动时检查当前模型与数据库记录是否一致
- 模型切换时自动重建 `memory_embeddings`
- 向量记录继续通过 `project_name` 做隔离

## 搜索结果裁剪规则

- `confidence == 1` 的命中始终返回
- `confidence < 1` 的错误记忆数量由 `search.low_confidence_error_hit_limit` 控制
- `confidence < 1` 的总结记忆数量由 `search.low_confidence_summary_hit_limit` 控制

## 兼容接口

兼容的接口形式：

```
POST <base_url>/embeddings
```
