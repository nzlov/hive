# 嵌入与语义检索

当 `embedding.baseUrl` 和 `embedding.model` 配置完整时，会启用语义检索；`apiKey` 可为空，以兼容本地无鉴权服务。

## 可选调优项

- `embedding.semanticSimilarityThreshold` 默认 `0.15`
- `embedding.semanticCandidateBatchSize` 默认 `256`
- `embedding.semanticCandidateMaxCount` 默认 `1024`
- `embedding.semanticHitFetchLimit` 默认 `64`
- `embedding.semanticSearchConcurrency` 默认 `4`
- `embedding.semanticWindow.mode` 默认 `static`，可切换到 `dynamic`
- `embedding.semanticWindow.baseMaxCount` 默认 `1024`
- `embedding.semanticWindow.dynamicMinCount` 默认 `256`
- `embedding.semanticWindow.dynamicMaxCount` 默认 `20000`
- `embedding.semanticWindow.dynamicRatio` 默认 `0.2`
- `embedding.decay.enabled` 默认 `true`
- `embedding.decay.ageWeight` 默认 `0.5`
- `embedding.decay.semanticWeight` 默认 `0.5`
- `embedding.decay.halfLifeDays.summary` 默认 `30`
- `embedding.decay.halfLifeDays.error` 默认 `90`

## 关键字与融合调优

- `search.keyword.mode` 默认 `like`，支持 `bm25`
- `search.keyword.bm25K1` 默认 `1.2`
- `search.keyword.bm25B` 默认 `0.75`
- `search.keyword.fields` / `search.keyword.fieldWeights` 用于 BM25 字段参与与权重
- `search.keyword.synonyms.enabled` / `search.keyword.synonyms.groups` 用于同义词扩展
- `search.fusion.enabled` 默认 `true`
- `search.fusion.keywordWeight` / `search.fusion.semanticWeight` / `search.fusion.recencyWeight` 用于融合权重
- `search.fusion.minSemanticScore` 用于过滤弱语义分

## 缓存调优

- `search.cache.enabled`：开启后缓存查询向量和语义命中
- `search.cache.queryEmbeddingTtlSeconds`：查询向量缓存时间
- `search.cache.semanticHitsTtlSeconds`：语义命中缓存时间
- `search.cache.maxEntries`：缓存容量上限
- `search.searchStageConcurrency`：搜索编排阶段并发数，控制 error/summary 关键字与语义任务并发度

## 启用后行为

- 写入时同步写入向量
- 搜索时在关键字命中外追加语义召回
- 查询侧会直接使用用户输入的 `description` 生成主查询向量
- 如果请求带有 `tags`，会额外把标签合并成一条辅助查询文本生成向量
- 关键字模式为 `bm25` 时，会按字段权重做关键字重排
- 融合开启时，会按关键字/语义/时效权重计算统一置信度
- 缓存开启时，会缓存查询向量和语义召回结果
- 服务启动时检查当前模型与数据库记录是否一致
- 模型切换时自动重建 `memory_embeddings`
- 向量记录继续通过 `project_name` 做隔离

## 缓存失效时机

- 调用写入接口新增记忆后，会清空搜索缓存
- 服务触发向量重建后，会清空搜索缓存

## 查询协议

- 搜索请求使用 `tags + description`
- `description` 必填，并直接作为 embedding 输入，不再套额外“问题描述”模板
- `tags` 可选，会参与关键字检索，并额外形成一条辅助向量查询文本
- 查询向量缓存和语义命中缓存都会把 `tags + description` 一起纳入缓存键

## 搜索结果裁剪规则

- `confidence == 1` 的命中始终返回
- `confidence < 1` 的错误记忆数量由 `search.lowConfidenceErrorHitLimit` 控制
- `confidence < 1` 的总结记忆数量由 `search.lowConfidenceSummaryHitLimit` 控制

## 兼容接口

兼容的接口形式：

```
POST <base_url>/embeddings
```
