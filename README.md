# Hive

`Hive` 是一个面向工程分析场景的项目记忆系统，使用 `Python CLI + Go HTTP Server + SQLite` 管理总结记忆与错误记忆。

当前版本采用单服务单库模型：

- 服务端配置只读取当前工作目录下的 `./config.json`
- 服务端只维护一份记忆库：`./.memory/memory.db`
- 不再区分项目内记忆与外挂记忆
- 多项目通过 `project_name` 做检索隔离

## 核心能力

- 支持两类记忆：`summary` 与 `error`
- 支持关键字检索与可选语义检索
- 支持批量写入多条记忆
- 支持记录 `git_branch`，客户端会过滤当前分支未合入的记忆
- 支持通过项目别名把同一服务端记忆库隔离为多个项目视图

## 目录结构

```text
.
├── cmd/hive-server/          # Go 服务端入口
├── internal/
│   ├── api/                  # HTTP 请求/响应结构
│   ├── config/               # 服务端配置加载
│   ├── memory/               # 存储、检索、向量与记忆构建逻辑
│   └── server/               # Gin 路由
├── hive/
│   ├── SKILL.md              # Skill 规则说明
│   ├── agents/openai.yaml    # Agent 展示元数据
│   ├── references/           # 记忆模板参考
│   └── scripts/main.py       # Python 客户端入口
└── README.md
```

## 工作方式

1. 客户端读取本地客户端配置，确定服务端地址与项目别名。
2. 客户端在本地推导 `project_name`，再把 `project_name`、查询词或写入内容通过 HTTP 发给服务端。
3. 服务端统一把所有项目的记忆写入同一个 SQLite 数据库。
4. 搜索时服务端按 `project_name` 严格过滤，只返回当前项目的数据。
5. 客户端再根据当前 Git 分支过滤未合入分支的历史记忆。

## 运行依赖

- Go `1.25+`
- Python `3.10+`
- 可选：OpenAI 兼容 Embeddings 服务

## 快速开始

### 1. 准备服务端配置

在服务端启动目录创建 `config.json`：

```json
{
  "server": {
    "base_url": "http://127.0.0.1:8080",
    "listen_addr": ":8080"
  },
  "embedding": {
    "base_url": "",
    "api_key": "",
    "model": "",
    "timeout_seconds": 30
  }
}
```

### 2. 启动服务端

```bash
go run ./cmd/hive-server
```

默认会在当前目录使用：

```text
./config.json
./.memory/memory.db
```

### 3. 健康检查

```bash
curl http://127.0.0.1:8080/healthz
```

返回：

```json
{"status":"ok"}
```

### 4. 客户端搜索示例

```bash
python3 hive/scripts/main.py search --root . --query "向量重建"
```

### 5. 客户端写入示例

```bash
python3 hive/scripts/main.py write \
  --root . \
  --items-json '[
    {
      "type": "summary",
      "title": "Hive 接入方式",
      "tags": ["hive", "接入"],
      "summary": "说明如何启动服务和调用客户端。",
      "context": "## Summary\n\n- 详情: Hive 通过 Python 脚本调用 Go 服务端。"
    }
  ]'
```

## 服务端配置

### 配置文件位置

服务端只读取当前工作目录下的：

```text
./config.json
```

不会再读取：

- `~/.config/hive/config.json`
- 项目目录下的其他配置文件
- 外挂记忆目录配置

### 服务端配置示例

```json
{
  "server": {
    "base_url": "http://127.0.0.1:8080",
    "listen_addr": ":8080"
  },
  "embedding": {
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-xxxx",
    "model": "text-embedding-3-small",
    "timeout_seconds": 30
  }
}
```

### 配置字段

- `server.base_url`：服务端对外访问地址，主要用于展示和客户端默认配置参考
- `server.listen_addr`：Gin 实际监听地址
- `embedding.base_url`：OpenAI 兼容 Embeddings 服务根地址
- `embedding.api_key`：嵌入服务认证令牌
- `embedding.model`：嵌入模型名
- `embedding.timeout_seconds`：嵌入请求超时秒数

### 服务端数据文件

服务端始终只维护一份数据库：

```text
./.memory/memory.db
```

这里的 `./` 指启动 `hive-server` 时的当前工作目录。

## 客户端配置

客户端配置仍由 Python 脚本读取，默认路径：

```text
~/.config/hive/config.json
```

客户端配置只负责：

- 决定默认请求哪个服务端
- 为不同项目指定不同服务地址
- 为不同项目指定 `project_name` / `alias`

客户端不控制服务端数据库位置。

### 客户端配置示例

```json
{
  "default_server_base_url": "http://127.0.0.1:8080",
  "projects": {
    "/home/dev/workspaces/payment-service": {
      "server_url": "http://127.0.0.1:8080",
      "alias": "payment-service"
    },
    "order-service": {
      "server": {
        "base_url": "http://127.0.0.1:18080"
      },
      "project_alias": "order-service-dev"
    }
  }
}
```

### 客户端字段

- `default_server_base_url`：默认请求地址
- `projects`：项目级覆盖配置
- `projects.<key>.server_url`：项目覆盖默认服务地址
- `projects.<key>.server.base_url`：同样可覆盖默认服务地址
- `projects.<key>.alias` / `project_alias`：请求里附带的 `project_name`

## 项目隔离规则

Hive 不再通过“每个项目各自一个数据库”来隔离，而是通过 `project_name` 在单库中隔离。

规则如下：

- 如果客户端配置了 `alias` / `project_alias`，则优先使用该值作为 `project_name`
- 如果未配置别名且当前目录是 Git 仓库，则使用 Git 仓库地址，并忽略协议差异
- 如果不是 Git 仓库，则回退到项目文件夹名称
- 写入和搜索都会使用同一套 `project_name` 规则
- 搜索时只返回当前 `project_name` 对应的记忆

这意味着两个项目即使共用同一个服务端和同一个数据库，只要 `project_name` 不同，结果就不会串项目。

## 服务端接口

### `GET /healthz`

健康检查。

### `POST /api/v1/memories/search`

请求示例：

```json
{
  "project_name": "payment-service",
  "queries": ["支付超时", "重试队列"],
  "debug": true
}
```

返回字段：

- `query`
- `project_name`
- `debug_commands`
- `error_hits`
- `summary_hits`
- `markdown`

### `POST /api/v1/memories/write`

请求示例：

```json
{
  "project_name": "payment-service",
  "git_branch": "feature/order-timeout",
  "items": [
    {
      "type": "error",
      "title": "支付超时排查",
      "tags": ["支付", "超时", "订单"],
      "summary": "记录支付超时的根因与修复结论。",
      "context": "## Summary\n\n- 详情: 超时由重试积压引起。"
    }
  ]
}
```

返回示例：

```json
{}
```

### `POST /api/v1/memories/rebuild-embeddings`

请求示例：

```json
{
  "force": true
}
```

返回示例：

```json
{
  "changed": true,
  "message": "已使用模型 text-embedding-3-small 重建 42 条向量。"
}
```

## 客户端使用说明

客户端入口：`hive/scripts/main.py`

### 搜索

```bash
python3 hive/scripts/main.py search --root . --query "关键词"
python3 hive/scripts/main.py search --root . --query "关键词1" --query "关键词2"
python3 hive/scripts/main.py search --root . --query '["关键词1","关键词2"]'
```

参数说明：

- `--root`：项目根目录，默认当前目录
- `--query`：必填，支持多个参数或 JSON 数组
- `--debug`：输出调试信息

### 写入

```bash
python3 hive/scripts/main.py write --root . --items-json '[
  {
    "type": "summary",
    "title": "订单对账流程",
    "tags": ["订单", "对账"],
    "summary": "记录对账流程中的关键约束。",
    "context": "## Summary\n\n- 详情: 对账任务依赖支付成功后的状态流转。"
  },
  {
    "type": "error",
    "title": "连接池耗尽",
    "tags": ["数据库", "连接池", "重试"],
    "summary": "记录连接泄漏的触发条件与修复方案。",
    "context": "## Summary\n\n- 详情: 异常路径未归还连接。"
  }
]'
```

约束：

- `--items-json` 必须是对象数组
- `type` 只能是 `summary` 或 `error`
- 每项都必须提供 `title` 和 `context`
- `tags` 支持数组，也兼容逗号分隔字符串

### 分支过滤

客户端会读取当前 Git 分支，并过滤掉尚未合入当前分支的历史记忆：

- 当前分支记忆：保留
- 已合入当前分支的历史分支记忆：保留
- 未合入当前分支的分支记忆：过滤
- 未记录分支的记忆：保留

## 嵌入与语义检索

当服务端 `embedding.base_url`、`embedding.api_key`、`embedding.model` 三项同时配置完整时，会启用语义检索。

启用后行为：

- 写入时同步写入向量
- 搜索时在关键字命中外追加语义召回
- 模型切换时自动重建全部向量
- 历史向量缺失时自动补建

嵌入服务需兼容 OpenAI Embeddings API：

```text
POST <base_url>/embeddings
```

## 记忆正文建议

### 总结记忆

```markdown
## Summary

- 详情: 先写核心结论。

## src/order/service.go

- 详情: 说明关键文件职责、依赖或约束。

## BuildOrderSnapshot

- 详情: 说明关键方法为什么这样设计。
```

### 错误记忆

```markdown
## Summary

- 详情: 描述错误现象和影响。

## src/pay/retry.go

- 详情: 标记问题出现位置与影响范围。

## RetryPayment

- 详情: 记录触发条件。

## 连接未归还导致连接池泄漏

- 根因: 异常路径缺少释放逻辑。
- 修复动作: 补充 `defer conn.Close()`。
- 验证结果: 压测后连接数恢复稳定。
```

参考模板：`hive/references/memory_template.md`

## 开发与测试

运行 Go 测试：

```bash
go test ./...
```

运行 Python 测试：

```bash
python3 -m unittest hive/scripts/test_main.py
```

## 常见问题

### 为什么不同项目会共用一个数据库？

- 这是当前版本的设计目标
- 服务端只维护 `./.memory/memory.db`
- 项目隔离不再依赖数据库文件路径，而是依赖 `project_name`

### 为什么搜索结果为空？

- 检查写入和搜索时使用的 `project_name` 是否一致
- 检查客户端 `alias` / `project_alias` 是否改动过
- 检查服务端是否连到了你预期的当前目录 `./config.json`

### 为什么服务端没有读取 `~/.config/hive/config.json`？

- 因为现在服务端只读取启动目录下的 `./config.json`
- `~/.config/hive/config.json` 仅作为客户端配置使用

## 仓库信息

- Go module：`github.com/nzlov/hive`
- 服务端入口：`cmd/hive-server`
- 客户端入口：`hive/scripts/main.py`
