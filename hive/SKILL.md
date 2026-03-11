---
name: hive
description: Trigger this skill by default for search or analysis tasks. prior-context recall, file/method/logic/module lookup, code reading, root-cause analysis, recurring-error troubleshooting, and continuation of earlier fixes. Always retrieve relevant memory before any code search, code analysis, or edits. Only write summary memories when the user explicitly asks to summarize and remember them, but always persist resolved session errors to avoid repeating the same mistake.
---

# 记忆管理

## 触发优先级

1. 只要任务包含“搜索”或“分析”语义，优先触发本技能并先检索记忆。
2. 以下场景视为默认触发：代码搜索、文件/方法定位、逻辑梳理、根因分析、错误排查、历史改动续接。
3. 即使用户未明确提到 memory，也应先执行一次记忆检索再进入代码检索或编辑。

## 检索记忆

1. 查询使用关键词或描述(尽量详细)，允许多个。
2. `--query` 同时支持多参数写法和 JSON 数组字符串写法。
3. 输出为 Markdown，结果按命中类型分为 `Error Hits` 和 `Summary Hits`，同一分组内多条记录用 `---` 分隔。
4. 单条记录通常包含：

- `title`
- tags
- timestamp
- confidence
- `snippets`（数组，展示 `line_range` 与 `content`）
- `file_content`（命中标题或需要完整上下文时返回）

说明：

- 实际排序以服务端返回结果为准，通常更接近相关性/置信度排序，而不是单纯按时间倒序。
- 脚本会按当前分支过滤未合并的分支记忆，避免把无关分支结果带进来。

使用命令：

```bash
python3 <hive目录>/scripts/main.py search --root '<项目根目录>' --query '["关键词","<描述>"]'
```

PowerShell：

```powershell
python <hive目录>/scripts/main.py search --root "<项目根目录>" --query '["关键词","<描述>"]'
```

cmd：

```cmd
python <hive目录>\scripts\main.py search --root "<项目根目录>" --query "[\"关键词\",\"<描述>\"]"
```

## 写入总结记忆

- 只有当用户明确要求“总结并记忆”“保存记忆”“写入总结记忆”等语义时，才调用脚本写入总结记忆。
- 总结整理记忆时一定要详细。
- 一次总结可以拆成多条记忆，按不同目标、问题、模块或结论分别写入，避免把无关内容混成一条。
- 如果当前会话里之前已经保存过记忆，那么再次总结时应从上次记忆之后的新内容开始续写，不要重复总结已保存部分。
- `context` 字段传入最终 Markdown 正文，注意 shell 环境转义。
- 使用具体实体作为小标题，不使用泛化栏目名。
- 小标题优先覆盖具体文件名、方法名、逻辑点（可补充模块、流程、结论）。
- 示例：`## src/order/service.go`、`## BuildOrderSnapshot`、`## 支付成功后进入已确认状态`。
- 每个实体小标题下至少包含一条“详情”信息（行为、约束、依赖、结论之一）。
- 推荐正文字段尽量稳定使用 `详情 / 结论 / 约束 / 依赖`，便于服务端 embedding 模板抽取关键语义。
- 脚本支持一次请求写入多条总结或错误记忆，适合把多个结论一起落库。

使用命令：

```bash
python3 <hive目录>/scripts/main.py write \
  --root '<项目根目录>' \
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

PowerShell（建议优先使用 `--items-file`，避免长 JSON 转义不稳定）：

```powershell
python <hive目录>/scripts/main.py write --root "<项目根目录>" --items-file ".\memory-items.json"
```

cmd（建议优先使用 `--items-file`，避免长 JSON 转义不稳定）：

```cmd
python <hive目录>\scripts\main.py write --root "<项目根目录>" --items-file ".\memory-items.json"
```

说明：

- `--items-json` 与 `--items-file` 必须二选一。
- 使用 `--items-file` 时，写入成功后脚本会自动删除该文件，避免敏感记忆内容残留在磁盘。

## 写入错误记忆

1. 出现错误先检索错误记忆。
2. 无可复用错误记忆且错误已解决时写入。
3. 会话中一旦发生错误且最终已经定位或修复，错误记忆必须写入，避免以后重复犯同样的问题。
4. 当总结记忆被触发且会话有错误时，错误记忆写入不可跳过。
5. 如果一次处理里有多个独立错误或多个根因，应拆成多条错误记忆分别写入。
6. 每条记忆的 `context` 字段需覆盖：`错误现象 / 触发条件 / 根因 / 修复动作 / 修复结论 / 验证结果`，这样更利于后端统一召回模板。

使用命令：

```bash
python3 <hive目录>/scripts/main.py write \
  --root '<项目根目录>' \
  --items-json '[
    {
      "type": "error",
      "title": "自动标题",
      "tags": ["业务标签", "错误类型", "相关模块"],
      "summary": "一句话简介",
      "context": "完整Markdown正文"
    }
  ]'
```

PowerShell：

```powershell
python <hive目录>/scripts/main.py write --root "<项目根目录>" --items-file ".\error-items.json"
```

cmd：

```cmd
python <hive目录>\scripts\main.py write --root "<项目根目录>" --items-file ".\error-items.json"
```

## 执行环境注意事项

1. `--items-json` 中若包含反引号 `` `...` ``，shell 可能误执行。
2. 建议用单引号包裹 `--items-json`；若 JSON 字符串内含单引号，先做 shell 转义。
3. 通过 OpenCode `bash` 工具调用时，始终提供 `command` 与 `description`。
4. Windows 下优先用 `python`（不是 `python3`），并优先使用 `--items-file`。
5. 复杂 JSON 建议写入临时文件后通过 `--items-file` 传入。临时文件写入成功会自动清理。
6. 注意脚本文件位置，所有的示例都是hive的相对路径，实际调用时使用全路径补全hive所在路径。

## 正文生成样例

1. 总结记忆正文模板（`context` 字段，可直接替换占位符）：

```markdown
# <文件或主题>

- 详情: <这个文件、模块或主题在做什么，为什么重要>
- 结论: <最终确认的事实、判断或沉淀出的经验>
- 约束: <使用前提、边界条件、限制项；没有可省略>
- 依赖: <相关文件、模块、配置或外部条件；没有可省略>

## <方法、流程或子主题1>

- 详情: <该方法或流程的关键行为>
- 结论: <这一段得出的结论>
- 约束: <这一段的限制或注意事项；没有可省略>
- 依赖: <这一段依赖的对象；没有可省略>

## <方法、流程或子主题2>

- 详情: <继续补充另一个关键点>
- 结论: <继续补充对应结论>
```

建议：优先稳定使用 `详情 / 结论 / 约束 / 依赖`，这样更利于后端 embedding 模板抽取关键信息。

1. 错误记忆正文模板（`context` 字段，可直接替换占位符）：

````markdown
# <文件或故障主题>

- 错误现象: <用户可见现象、报错或异常表现>
- 触发条件: <在什么输入、环境、流量或时序下触发>
- 根因: <最终定位出的真正原因>
- 修复动作: <采取了什么修复措施>
- 修复结论: <修复后的状态或行为变化>
- 验证结果: <如何验证，结果如何>
- 影响: <影响范围、风险或受影响模块；没有可省略>

## <相关方法、链路或排查点1>

- 详情: <这一段排查到的事实>
- 触发条件: <这一段对应的具体触发条件；没有可省略>
- 根因: <如果这里已经能落到根因则写明；没有可省略>
- 修复动作: <如果这里已有明确修复动作则写明；没有可省略>

## <相关方法、链路或排查点2>

- 根因: <继续补充更具体的根因>
- 修复动作: <继续补充修复动作>
- 修复结论: <继续补充修复结果>
- 验证结果: <继续补充验证方式与结果>

## Original Error Code

```text
<可选：放修复前的关键错误代码；没有可整段删除>
```

## Fixed Code

```text
<可选：放修复后的关键代码；没有可整段删除>
```
````

建议：错误记忆优先稳定覆盖 `错误现象 / 触发条件 / 根因 / 修复动作 / 修复结论 / 验证结果`，这样更利于后端统一召回模板。
