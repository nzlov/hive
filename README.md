# memory-manager

`memory-manager` 是一个面向工程分析场景的记忆管理 skill，用来在代码搜索、问题分析、错误排查前后读写项目记忆。

## 适用场景

- 在开始代码搜索、文件定位、逻辑梳理前，先检索历史记忆。
- 在问题解决后，写入可复用的总结记忆。
- 在错误修复后，补充错误记忆，记录现象、根因和修复结论。
- 在多个项目间复用统一记忆目录时，通过配置开启外挂记忆。

## 目录结构

当前 skill 主要文件：

- `SKILL.md`：skill 规则与约束。
- `scripts/search_memory.py`：检索记忆。
- `scripts/write_memory.py`：写入总结或错误记忆。
- `scripts/memory_config.py`：解析外挂记忆配置。

默认记忆目录结构：

```text
.memory/
  memory.db
```

## 如何检索记忆

在项目根目录执行：

```bash
python3 scripts/search_memory.py --query '关键词'
python3 scripts/search_memory.py --query '关键词1' '关键词2'
python3 scripts/search_memory.py --query '["关键词1","关键词2"]'
```

常用参数：

- `--root`：指定项目根目录，默认当前目录。
- `--query`：必填，支持多个关键词或 JSON 数组。
- `--error-limit`：返回错误记忆数量，默认 `5`。
- `--summary-limit`：返回总结记忆数量，默认 `10`。
- `--debug`：输出实际执行的搜索命令。

返回结果包含：

- `query`：本次查询词。
- `search_root`：本次实际搜索根目录。
- `Error Hits` / `Summary Hits`：命中的错误记忆与总结记忆。
- `snippets`：命中的片段与行号。
- `file_content`：当标题命中时返回完整记忆内容。
- `project`：写入和检索时都会自动附带当前项目名，用于共享库隔离。

## 如何写入总结记忆

```bash
python3 scripts/write_memory.py \
  --type summary \
  --title '自动标题' \
  --tags '业务标签,重要文件,重要方法' \
  --summary '一句话简介' \
  --context '完整Markdown正文'
```

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

```bash
python3 scripts/write_memory.py \
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

示例配置：

```json
{
  "memory_storage_path": "/data/memories",
  "external_projects": [
    "vivid-beaver",
    {
      "path": "/abs/path/to/another-project"
    }
  ]
}
```

字段说明：

- `memory_storage_path`：外挂记忆根目录。
- `external_projects`：需要启用外挂记忆的项目列表。

`external_projects` 支持以下写法：

- 直接写项目名，例如 `"vivid-beaver"`
- 直接写项目根路径，例如 `"/abs/path/to/project"`
- 对象格式，例如 `{ "name": "vivid-beaver" }`
- 对象格式，例如 `{ "path": "/abs/path/to/project" }`

命中外挂配置后，实际记忆目录会切换为：

```text
记忆存储路径/.memory/memory.db
```

例如：

```text
/data/memories/.memory/memory.db
```

此时所有外挂项目共用同一个 SQLite 文件，但每条记录都会自动写入当前项目名；检索时也会自动按当前项目名过滤，因此不会串项目。

如果未命中外挂配置，脚本仍然使用项目根目录下的：

```text
.memory/
```

## 使用建议

- 在任何搜索、分析、排查任务开始前先执行一次检索。
- `--context` 建议使用 Markdown，方便后续按标题检索。
- 标题尽量使用具体文件名、方法名、业务结论，避免泛化标题。
- 若 shell 参数中包含反引号或单引号，注意转义。
- 配置文件读取失败时，脚本会自动回退到项目内 `.memory/`，不会中断执行。

## 一个完整流程示例

1. 检索历史记忆。
2. 进行代码搜索、分析或修复。
3. 问题解决后写入总结记忆。
4. 如果过程中出现过错误并已修复，再补写错误记忆。

示例：

```bash
python3 scripts/search_memory.py --query '["订单","支付","超时"]'

python3 scripts/write_memory.py \
  --type summary \
  --title '支付超时排查结论' \
  --tags '支付,超时,订单' \
  --summary '记录支付超时排查后的核心结论。' \
  --context '## Summary

- 详情: 支付超时主要由重试任务堆积导致。

## retry_worker.py

- 详情: 重试队列消费速度不足是核心瓶颈。'
```
