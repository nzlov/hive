---
name: memory-manager
description: Trigger this skill by default for search or analysis tasks. prior-context recall, file/method/logic/module lookup, code reading, root-cause analysis, recurring-error troubleshooting, and continuation of earlier fixes. Always retrieve relevant memory before any code search, code analysis, or edits. Only write summary memories when the user explicitly asks to summarize and remember them, but always persist resolved session errors to avoid repeating the same mistake.
---

# 记忆管理

## 头部规范

1. 时间信息仅来自文件名时间戳 `YYYYMMDDHHMMSS`。
2. 头部信息有：`type`、`title`、`tags`、`summary`。

## 触发优先级

1. 只要任务包含“搜索”或“分析”语义，优先触发本技能并先检索记忆。
2. 以下场景视为默认触发：代码搜索、文件/方法定位、逻辑梳理、根因分析、错误排查、历史改动续接。
3. 即使用户未明确提到 memory，也应先执行一次记忆检索再进入代码检索或编辑。

## 检索记忆

1. 查询支持数组：`--query a b` 或 `--query '["a","b"]'`。
2. 按时间倒序（新到旧）。
3. 输出为 Markdown，多条记录用 `---` 分隔。
4. 返回包含：

- `query`
- `search_root`
- 命中记录（含 `source/path/timestamp/confidence`）
- `snippets`（数组，含 `line_range.start/end` 与 `content`）
- `file_content`（仅 `title` 命中时返回完整文件）

使用命令：

```bash
python3 scripts/search_memory.py --query '关键词'
python3 scripts/search_memory.py --query '关键词1' '<关键词2>'
python3 scripts/search_memory.py --query '["关键词1","<关键词2>"]'
```

## 写入总结记忆

- 只有当用户明确要求“总结并记忆”“保存记忆”“写入总结记忆”等语义时，才调用脚本写入总结记忆。
- 一次总结可以拆成多条记忆，按不同目标、问题、模块或结论分别写入，避免把无关内容混成一条。
- 如果当前会话里之前已经保存过记忆，那么再次总结时应从上次记忆之后的新内容开始续写，不要重复总结已保存部分。
- `--context` 传入最终 Markdown 正文，注意shell环境转义。
- 使用具体实体作为小标题，不使用泛化栏目名。
- 小标题优先覆盖具体文件名、方法名、逻辑点（可补充模块、流程、结论）。
- 示例：`## src/order/service.go`、`## BuildOrderSnapshot`、`## 支付成功后进入已确认状态`。
- 每个实体小标题下至少包含一条“详情”信息（行为、约束、依赖、结论之一）。
- 脚本支持一次请求写入多条总结或错误记忆，适合把多个结论一起落库。

使用命令：

* 单条记忆
```bash
python3 scripts/write_memory.py \
  --type summary \
  --title '自动标题' \
  --tags '业务标签,重要文件,重要方法' \
  --summary '一句话简介' \
  --context '完整Markdown正文'
```

* 多条记忆
```bash
python3 scripts/write_memory.py \
  --items-json '[
    {
      "type": "summary",
      "title": "自动标题1",
      "tags": ["业务标签", "文件A"],
      "summary": "一句话简介1",
      "context": "完整Markdown正文1"
    },
    {
      "type": "summary",
      "title": "自动标题2",
      "tags": ["业务标签", "文件B"],
      "summary": "一句话简介2",
      "context": "完整Markdown正文2"
    }
  ]'
```

## 写入错误记忆

1. 出现错误先检索错误记忆。
2. 无可复用错误记忆且错误已解决时写入。
3. 会话中一旦发生错误且最终已经定位或修复，错误记忆必须写入，避免以后重复犯同样的问题。
4. 当总结记忆被触发且会话有错误时，错误记忆写入不可跳过。
5. 如果一次处理里有多个独立错误或多个根因，应拆成多条错误记忆分别写入。
6. `--context` 需覆盖：错误现象、触发条件、根因、修复结论。

使用命令：

```bash
python3 scripts/write_memory.py \
  --type error \
  --title '自动标题' \
  --tags '业务标签,错误类型,相关模块' \
  --summary '一句话简介' \
  --context '完整Markdown正文'
```

## Shell 注意事项

1. `--context` 中若包含反引号 `` `...` ``，shell 可能误执行。
2. 建议用单引号包裹 `--context`；若正文含单引号，先做 shell 转义。
3. 通过 OpenCode `bash` 工具调用时，始终提供 `command` 与 `description`。

## 正文生成样例

1. 总结记忆正文样例（`--context`）：

```markdown
## Summary

- 详情: 订单支付成功后由异步任务更新对账状态，避免主链路阻塞。

## src/order/reconcile_service.go

- 详情: 对账入口文件，负责拉取待对账订单并分批处理。
- 依赖: `internal/reconcile/repo.go`

## RunDailyReconcile

- 详情: 每日 00:30 触发全量对账，失败任务进入重试队列。
- 约束: 单次批量上限 500，避免长事务。

## 支付成功后进入已确认状态

- 详情: 状态流转为 `PAID -> CONFIRMED`，并写入审计日志。
- 结论: 该流转是对账任务的前置条件。
```

2. 错误记忆正文样例（`--context`）：

````markdown
## Summary

- 详情: 高并发下数据库连接未释放，导致连接池耗尽。

## src/pay/retry.go

- 详情: 重试流程中异常分支提前返回，遗漏连接关闭。
- 影响: 支付重试请求在峰值时大量失败。

## RetryPayment

- 详情: `tx.Rollback()` 未在所有异常路径执行。
- 触发条件: QPS > 800 且下游超时比例升高。

## 连接未归还导致连接池泄漏

- 根因: 错误路径缺少 `defer conn.Close()`。
- 修复动作: 在连接创建后立即 `defer conn.Close()`，并补齐异常分支回滚。
- 验证结果: 压测 30 分钟连接数稳定，错误率从 12% 降至 0.3%。

## Original Error Code

```text
conn, _ := db.Get()
if err != nil {
    return err
}
```

## Fixed Code

```text
conn, err := db.Get()
if err != nil {
    return err
}
defer conn.Close()
```
````
