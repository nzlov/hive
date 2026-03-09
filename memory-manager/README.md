# memory-manager

`memory-manager` 是一个面向工程分析场景的记忆管理 skill，用来在代码搜索、问题分析、错误排查前后读写项目记忆。

现在它也支持可选的嵌入检索：在保留原有关键字写入与查询模式的前提下，如果配置了嵌入模型，脚本会同时维护记忆向量，并在查询时补充语义召回。

## 适用场景

- 在开始代码搜索、文件定位、逻辑梳理前，先检索历史记忆。
- 在问题解决后，写入可复用的总结记忆。
- 在错误修复后，补充错误记忆，记录现象、根因和修复结论。
- 在多个项目间复用统一记忆目录时，通过配置开启外挂记忆。
- 在配置嵌入模型后，同时使用 OpenAI 兼容接口生成向量，提升不同措辞下的命中率。

## 目录结构

当前 skill 主要文件：

- `SKILL.md`：skill 规则与约束。
- `scripts/main.go`：Go 统一入口，通过子命令执行检索、写入和重建。
- `scripts/memory_config.py`：解析外挂记忆配置。
- `scripts/rebuild_memory_embeddings.go`：重建全部记忆向量。
- `scripts/embedding_provider.py`：嵌入接口与 OpenAI 兼容实现。

默认记忆目录结构：

```text
.memory/
  memory.db
```

数据库会在需要时自动补充两类表：

- `memory_embeddings`：保存每条记忆对应的向量。
- `memory_metadata`：保存当前数据库对应的嵌入模型名。

其中数据库只保存嵌入模型名，不保存服务地址和 `api_key`；服务端配置仍然只来自本地配置文件。

## 如何检索记忆

在项目根目录执行：

```bash
go run ./scripts search-memory --query '关键词'
go run ./scripts search-memory --query '关键词1' --query '关键词2'
go run ./scripts search-memory --query '["关键词1","关键词2"]'
```

常用参数：

- `--root`：指定项目根目录，默认当前目录。
- `--query`：必填，支持多个关键词或 JSON 数组。
- `--debug`：输出实际执行的搜索命令。

返回结果包含：

- `query`：本次查询词。
- `search_root`：本次实际搜索根目录。
- `Error Hits` / `Summary Hits`：命中的错误记忆与总结记忆。
- `snippets`：命中的片段与行号。
- `file_content`：当标题命中时返回完整记忆内容。
- `project`：写入和检索时都会自动附带当前项目名，用于共享库隔离。

如果配置了嵌入模型，查询时会在原有关键字匹配之外额外进行语义召回，并自动合并结果；当前查询不再限制返回条数。

## 如何写入总结记忆

总结记忆建议遵循这些规则：

- 只有在用户明确要求“总结并记忆”时再写入总结记忆，避免把普通执行结果都落成长期记忆。
- 一次总结可以拆成多条记忆，分别覆盖不同目标、问题、模块或结论。
- 如果当前会话里之前已经保存过记忆，再次总结时应从上次已保存内容之后开始续写，避免重复总结。

```bash
go run ./scripts write-memory \
  --type summary \
  --title '自动标题' \
  --tags '业务标签,重要文件,重要方法' \
  --summary '一句话简介' \
  --context '完整Markdown正文'

go run ./scripts write-memory \
  --items-json '[
    {
      "type": "summary",
      "title": "支付超时排查结论",
      "tags": ["支付", "超时", "订单"],
      "summary": "记录支付超时排查后的核心结论。",
      "context": "## Summary\n\n- 详情: 支付超时主要由重试任务堆积导致。"
    },
    {
      "type": "summary",
      "title": "重试队列优化建议",
      "tags": ["支付", "重试", "性能"],
      "summary": "补充后续优化方向。",
      "context": "## Summary\n\n- 详情: 需要扩容消费者并限制单批任务数量。"
    }
  ]'
```

`--items-json` 支持对象或对象数组，单次请求里可以混合写入多条 `summary` / `error` 记忆。

建议正文结构：

```markdown
## Summary

- 详情: 先写本次结论。

## src/example.py

- 详情: 说明关键文件承担的职责、约束或依赖。

## ExampleMethod

- 详情: 说明关键逻辑为什么这样实现。
```

## 如何写入错误记忆

错误记忆规则：

- 会话里出现的错误，只要最终已定位或修复，就必须写入错误记忆。
- 如果一次处理里有多个独立错误或多个问题目标，建议拆成多条错误记忆分别保存。

```bash
go run ./scripts write-memory \
  --type error \
  --title '自动标题' \
  --tags '业务标签,错误类型,相关模块' \
  --summary '一句话简介' \
  --context '完整Markdown正文'
```

建议至少包含：

- 错误现象
- 触发条件
- 根因
- 修复结论
- 验证结果

## 外挂记忆配置

脚本会自动检测：

```text
~/.config/memorymanager/config.json
```

如果配置文件不存在，脚本会自动创建默认配置：

```json
{
  "memory_storage_path": "/home/当前用户/.local/share/memorymanager",
  "embedding": {
    "base_url": "",
    "api_key": "",
    "model": "",
    "timeout_seconds": 30
  }
}
```

对应的默认外挂记忆数据库文件为：

```text
~/.local/share/memorymanager/.memory/memory.db
```

示例配置：

```json
{
  "memory_storage_path": "/data/memories",
  "embedding": {
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-xxxx",
    "model": "text-embedding-3-small",
    "timeout_seconds": 30
  }
}
```

字段说明：

- `memory_storage_path`：外挂记忆根目录。
- `embedding.base_url`：嵌入服务地址，使用 OpenAI Embeddings API 路径结构。
- `embedding.api_key`：嵌入服务的认证令牌。
- `embedding.model`：要使用的嵌入模型名。
- `embedding.timeout_seconds`：请求超时时间，默认 `30` 秒。

记忆位置规则如下：

- 如果当前项目根目录已经存在 `.memory/`，脚本使用当前项目根目录下的 `./.memory/memory.db`
- 如果当前项目根目录不存在 `.memory/`，脚本回退到外挂记忆目录下的共享数据库

回退到外挂目录后，实际记忆目录为：

```text
记忆存储路径/.memory/memory.db
```

例如：

```text
/data/memories/.memory/memory.db
```

此时所有外挂项目共用同一个 SQLite 文件，但每条记录都会自动写入当前项目名；检索时也会自动按当前项目名过滤，因此不会串项目。

## 嵌入模型工作方式

- 如果未配置 `embedding` 或配置不完整，脚本继续只使用原有关键字写入与查询流程。
- 如果配置了 `embedding`，`write_memory.py` 在写入记忆后会同步写入该条记录的向量。
- 如果配置了 `embedding`，`search_memory.py` 在关键字检索之外还会追加语义召回，并合并结果返回。
- 每次脚本启动时都会检查配置文件中的嵌入模型是否与数据库元数据中的模型名一致。
- 如果模型名不一致，脚本会自动触发 `go run ./scripts rebuild-memory-embeddings` 对现有记忆做全量重建，并把数据库中的模型名更新为当前配置。
- 如果模型名未变化但历史向量缺失，脚本也会自动补建，避免升级后出现部分记录没有向量。
- 嵌入文本会把标签整理为自然语言而不是 JSON，并剔除持久化内容里的 YAML 头部，减少重复噪声。
- 总结记忆和错误记忆使用不同模板：前者突出主题、摘要和结论，后者突出问题、现象摘要和排障记录。

可以手动执行全量重建：

```bash
go run ./scripts rebuild-memory-embeddings --root .
go run ./scripts rebuild-memory-embeddings --root . --force
```

## 使用建议

- 在任何搜索、分析、排查任务开始前先执行一次检索。
- `--context` 建议使用 Markdown，方便后续按标题检索。
- 标题尽量使用具体文件名、方法名、业务结论，避免泛化标题。
- 若 shell 参数中包含反引号或单引号，注意转义。
- 如果希望项目独立存储记忆，先在项目根目录创建 `.memory/` 目录。
- 配置文件读取失败时，脚本会回退到默认外挂目录配置或项目内 `.memory/`，不会中断执行。
- 嵌入模型切换后无需手动迁移，脚本会在启动时自动完成向量重建。

## 一个完整流程示例

1. 检索历史记忆。
2. 进行代码搜索、分析或修复。
3. 只有在用户明确要求总结并记忆时，才写入总结记忆；必要时可一次写入多条。
4. 如果过程中出现过错误并已修复，再补写错误记忆；这一步不可跳过。

示例：

```bash
go run ./scripts search-memory --query '["订单","支付","超时"]'

go run ./scripts write-memory \
  --type summary \
  --title '支付超时排查结论' \
  --tags '支付,超时,订单' \
  --summary '记录支付超时排查后的核心结论。' \
  --context '## Summary

- 详情: 支付超时主要由重试任务堆积导致。

## retry_worker.py

- 详情: 重试队列消费速度不足是核心瓶颈。'
```
