# 嵌入与语义检索

当 `embedding.base_url` 和 `embedding.model` 配置完整时，会启用语义检索；`api_key` 可为空，以兼容本地无鉴权服务。

## 可选调优项

- `embedding.semantic_similarity_threshold` 默认 `0.15`
- `embedding.semantic_candidate_batch_size` 默认 `256`
- `embedding.semantic_candidate_max_count` 默认 `1024`
- `embedding.semantic_hit_fetch_limit` 默认 `64`
- `embedding.semantic_window.mode` 默认 `static`，可切换到 `dynamic`
- `embedding.semantic_window.base_max_count` 默认 `1024`
- `embedding.semantic_window.dynamic_min_count` 默认 `256`
- `embedding.semantic_window.dynamic_max_count` 默认 `20000`
- `embedding.semantic_window.dynamic_ratio` 默认 `0.2`
- `embedding.decay.enabled` 默认 `true`
- `embedding.decay.age_weight` 默认 `0.5`
- `embedding.decay.semantic_weight` 默认 `0.5`
- `embedding.decay.half_life_days.summary` 默认 `30`
- `embedding.decay.half_life_days.error` 默认 `90`

## 关键字与融合调优

- `search.keyword.mode` 默认 `like`，支持 `bm25`
- `search.keyword.bm25_k1` 默认 `1.2`
- `search.keyword.bm25_b` 默认 `0.75`
- `search.keyword.fields` / `search.keyword.field_weights` 用于 BM25 字段参与与权重
- `search.keyword.synonyms.enabled` / `search.keyword.synonyms.groups` 用于同义词扩展
- `search.fusion.enabled` 默认 `true`
- `search.fusion.keyword_weight` / `search.fusion.semantic_weight` / `search.fusion.recency_weight` 用于融合权重
- `search.fusion.min_semantic_score` 用于过滤弱语义分

## 缓存调优

- `search.cache.enabled`：开启后缓存查询向量和语义命中
- `search.cache.query_embedding_ttl_seconds`：查询向量缓存时间
- `search.cache.semantic_hits_ttl_seconds`：语义命中缓存时间
- `search.cache.max_entries`：缓存容量上限

## 启用后行为

- 写入时同步写入向量
- 搜索时在关键字命中外追加语义召回
- 关键字模式为 `bm25` 时，会按字段权重做关键字重排
- 融合开启时，会按关键字/语义/时效权重计算统一置信度
- 缓存开启时，会缓存查询向量和语义召回结果
- 服务启动时检查当前模型与数据库记录是否一致
- 模型切换时自动重建 `memory_embeddings`
- 向量记录继续通过 `project_name` 做隔离

## 缓存失效时机

- 调用写入接口新增记忆后，会清空搜索缓存
- 服务触发向量重建后，会清空搜索缓存

## 搜索结果裁剪规则

- `confidence == 1` 的命中始终返回
- `confidence < 1` 的错误记忆数量由 `search.low_confidence_error_hit_limit` 控制
- `confidence < 1` 的总结记忆数量由 `search.low_confidence_summary_hit_limit` 控制

## 兼容接口

兼容的接口形式：

```
POST <base_url>/embeddings
```
