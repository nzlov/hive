# Hive Skill 使用说明

Hive 是一个面向工程分析场景的项目记忆系统，集成到 OpenCode 作为 Skill 使用。

## 什么是 Hive Skill

Hive Skill 是 OpenCode 的一个技能模块，专门用于：
- 记忆检索：在代码搜索、分析前自动检索相关记忆
- 记忆写入：自动记录总结和错误，避免重复踩坑

## 触发场景

### 自动触发

以下场景会自动触发 Hive Skill 并先执行记忆检索：

- 代码搜索
- 文件/方法定位
- 逻辑梳理
- 根因分析
- 错误排查
- 历史改动续接

### 手动触发

只有当用户明确要求"总结并记忆""保存记忆""写入总结记忆"时，才会执行写入操作。

## 客户端配置说明

Hive 客户端脚本读取本地配置文件：

```text
~/.config/hive/config.json
```

当文件不存在时，脚本会自动创建最小配置骨架：

```json
{
  "defaultServerBaseUrl": "http://127.0.0.1:8080",
  "apiToken": "",
  "projects": {}
}
```

### 完整配置示例

```json
{
  "defaultServerBaseUrl": "http://127.0.0.1:8080",
  "apiToken": "global-token",
  "projects": {
    "/home/dev/work/repo-a": {
      "projectName": "team/repo-a",
      "baseUrl": "http://127.0.0.1:8080",
      "apiToken": "repo-a-token"
    },
    "github.com/acme/repo-b.git": {
      "projectName": "acme/repo-b",
      "baseUrl": "https://hive.example.com",
      "apiToken": "repo-b-token"
    }
  }
}
```

### 字段说明（推荐最简键名）

- 服务端地址：
  - 全局默认使用 `defaultServerBaseUrl`
  - 项目级覆盖使用 `baseUrl`（仅一级键，不使用嵌套 `server.baseUrl`）
- API Token：
  - 项目级优先，其次全局
  - 推荐统一使用 `apiToken`
  - 未配置 Token 时，`search` / `write` 会直接失败
- 项目标识（写入 `project_name`）：
  - 项目级使用 `projectName`
  - 若未配置，优先使用 Git 远端仓库标识（如 `github.com/org/repo.git`），再回退到目录名

### `projects` 匹配规则

脚本会基于 `--root` 解析绝对路径，并按以下顺序命中 `projects` 键：

1. 项目绝对路径（如 `/home/dev/work/repo-a`）
2. Git 远端仓库标识（如 `github.com/acme/repo-b.git`）
3. 目录名（如 `repo-a`）

建议优先使用“绝对路径”或“Git 远端仓库标识”作为键，避免多仓库重名导致配置串用。

## 检索记忆

使用关键词或描述进行查询，支持多个查询词，按时间倒序返回。

### 命令格式

**Bash**
```bash
python3 hive/scripts/main.py search --root '<项目根目录>' --query '["关键词","<描述>"]'
```

**PowerShell**
```powershell
python hive/scripts/main.py search --root "<项目根目录>" --query '["关键词","<描述>"]'
```

**cmd**
```cmd
python hive/scripts\main.py search --root "<项目根目录>" --query "[\"关键词\",\"<描述>\"]"
```

### 返回字段

- `query`：查询关键词
- `project_name`：项目名称
- `tags`：标签
- `timestamp`：时间戳
- `confidence`：置信度
- `snippets`：匹配的代码片段（含行号范围）
- `file_content`：仅 title 命中时返回完整文件内容

## 写入总结记忆

只有当用户明确要求"总结并记忆""保存记忆""写入总结记忆"时才执行写入。

### 写入规则

1. 总结内容要详细
2. 一次总结可拆成多条记忆，按不同目标、问题、模块或结论分别写入
3. 避免把无关内容混成一条
4. 如果会话中已保存过记忆，再次总结时应从上次记忆之后的新内容开始续写
5. 使用具体实体作为小标题（文件名、方法名、逻辑点），不使用泛化栏目名
6. 每个实体小标题下至少包含一条"详情"信息

### 命令格式

**Bash**
```bash
python3 hive/scripts/main.py write \
  --root '<项目根目录>' \
  --items-json '[
    {
      "type": "summary",
      "title": "自动标题1",
      "tags": ["业务标签", "文件A"],
      "summary": "一句话简介1",
      "context": "完整Markdown正文1"
    }
  ]'
```

**PowerShell / cmd（推荐使用文件方式）**
```powershell
python hive/scripts/main.py write --root "<项目根目录>" --items-file ".\memory-items.json"
```

### 参数说明

- `--items-json` 与 `--items-file` 必须二选一
- 使用 `--items-file` 时，写入成功后脚本会自动删除文件，避免敏感内容残留

## 写入错误记忆

### 写入规则

1. 出现错误先检索错误记忆
2. 无可复用错误记忆且错误已解决时写入
3. 会话中一旦发生错误且最终已定位或修复，错误记忆必须写入
4. 当总结记忆被触发且会话有错误时，错误记忆写入不可跳过
5. 一次处理中有多个独立错误或根因，应拆成多条错误记忆分别写入
6. 每条记忆的 `context` 需覆盖：错误现象、触发条件、根因、修复结论

### 命令格式

**Bash**
```bash
python3 hive/scripts/main.py write \
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

## 正文生成样例

### 总结记忆正文

```markdown
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

### 错误记忆正文

```markdown
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
```

## 注意事项

1. `--items-json` 中若包含反引号，shell 可能误执行，建议用单引号包裹
2. Windows 下优先用 `python`（不是 `python3`），并优先使用 `--items-file`
3. 复杂 JSON 建议写入临时文件后通过 `--items-file` 传入，临时文件会自动清理

## 相关文档

- [快速开始](./getting-started.md)
- [服务端配置](./server-config.md)
- [服务端接口](./api.md)
- [嵌入与语义检索](./embedding.md)
