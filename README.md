# Hive

`Hive` 是一个面向工程分析场景的项目记忆系统，使用 `Python CLI + Go HTTP Server + Vue Admin SPA` 管理总结记忆与错误记忆。

当前版本的后端存储已经统一到 `GORM`，支持：

- `SQLite`：默认模式，数据文件位于 `./.memory/memory.db`
- `PostgreSQL`：通过 `config.json` 显式配置 `driver + dsn`

数据库访问统一收口在 `internal/models`，业务层不暴露 `sql.DB` 或 `gorm.DB`。

## 核心能力

- 支持两类记忆：`summary` 与 `error`
- 支持关键字检索与可选语义检索
- 支持批量写入多条记忆
- 支持通过 `project_name` 做单库项目隔离
- 支持记录 `git_branch`，客户端可过滤未合入分支的历史记忆
- 支持用户管理、JWT 登录和 API Token 鉴权
- 支持在记忆写入时记录创建用户 `userid(UUID)`

## 目录结构

```text
.
├── cmd/hive-server/          # Go 服务端入口
├── internal/
│   ├── api/                  # HTTP 请求/响应结构
│   ├── config/               # 配置加载与标准化
│   ├── memory/               # 记忆搜索、写入、向量与业务逻辑
│   ├── models/               # GORM 模型与数据库访问封装
│   ├── server/               # Gin 路由与中间件
│   └── user/                 # 用户、JWT 与 API Token 能力
├── hive/
│   ├── SKILL.md
│   ├── agents/openai.yaml
│   ├── references/
│   └── scripts/main.py       # Python 客户端入口
├── web/                      # Vue 管理后台
├── Markfile
└── README.md
```

## 工作方式

1. 客户端读取本地配置，确定服务端地址、项目别名和 `api_token`
2. 客户端把 `project_name`、查询词或记忆内容通过 HTTP 发给服务端
3. 服务端把所有项目记忆写入同一个数据库
4. 搜索时服务端按 `project_name` 严格过滤结果
5. 客户端可再按当前 Git 分支过滤未合入记忆

## 运行依赖

- Go `1.25+`
- Node.js `20+`
- Python `3.10+`
- 可选：OpenAI 兼容 Embeddings 服务
- PostgreSQL 模式下需要可访问的 PostgreSQL 实例

## 快速开始

### 1. 准备服务端配置

在服务端启动目录创建 `config.json`：

```json
{
  "server": {
    "base_url": "http://127.0.0.1:8080",
    "listen_addr": ":8080"
  },
  "auth": {
    "jwt_secret": "please-change-this-secret"
  },
  "database": {
    "driver": "sqlite",
    "dsn": ""
  },
  "embedding": {
    "base_url": "",
    "api_key": "",
    "model": "",
    "timeout_seconds": 30
  }
}
```

说明：

- `database.driver` 为空或缺失时，默认回落到 `SQLite`
- `SQLite` 默认使用 `./.memory/memory.db`
- `PostgreSQL` 需要显式设置 `database.driver=postgresql` 和 `database.dsn`

### 2. 启动服务端

```bash
go run ./cmd/hive-server
```

或使用：

```bash
make -f Markfile dev-server
```

首次启动如果数据库里没有任何用户，服务端会自动创建默认管理员：

- 用户名：`admin`
- 真实名称：`系统管理员`
- `userid`：自动生成 `UUID`
- 密码：随机生成，并打印在启动日志中
- `apitoken`：自动生成，可在管理后台查看

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

### 6. 启动前端开发环境

```bash
make -f Markfile dev-ui
```

前端默认监听 `http://127.0.0.1:5173`，并通过 Vite 代理转发：

- `/api/*` -> `http://127.0.0.1:8080`
- `/tokenapi/*` -> `http://127.0.0.1:8080`

## 服务端配置

### 配置文件位置

服务端只读取当前工作目录下的：

```text
./config.json
```

### SQLite 配置

```json
{
  "database": {
    "driver": "sqlite",
    "dsn": ""
  }
}
```

当 `dsn` 为空时，默认使用：

```text
./.memory/memory.db
```

### PostgreSQL 配置

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

### 配置字段

- `server.base_url`：服务端对外访问地址
- `server.listen_addr`：Gin 实际监听地址
- `auth.jwt_secret`：管理后台 JWT 签名密钥
- `database.driver`：数据库驱动，支持 `sqlite` / `postgresql`
- `database.dsn`：数据库连接串；SQLite 为空时使用默认文件路径
- `embedding.base_url`：OpenAI 兼容 Embeddings 服务根地址
- `embedding.api_key`：嵌入服务认证令牌
- `embedding.model`：嵌入模型名
- `embedding.timeout_seconds`：嵌入请求超时秒数

## 客户端配置

客户端默认读取：

```text
~/.config/hive/config.json
```

客户端配置负责：

- 决定默认请求哪个服务端
- 决定默认使用哪个 `api_token`
- 为不同项目指定不同服务地址
- 为不同项目指定 `project_name` / `alias`
- 为不同项目覆盖独立 `api_token`

客户端不控制服务端数据库位置。

### 客户端配置示例

```json
{
  "default_server_base_url": "http://127.0.0.1:8080",
  "api_token": "default-api-token",
  "projects": {
    "/home/dev/workspaces/payment-service": {
      "server_url": "http://127.0.0.1:8080",
      "alias": "payment-service",
      "api_token": "payment-service-token"
    }
  }
}
```

## 项目隔离规则

Hive 通过 `project_name` 在单库中隔离项目，而不是通过“每个项目一个数据库”隔离。

- 如果客户端配置了 `alias` / `project_alias`，优先使用它作为 `project_name`
- 如果未配置别名且当前目录是 Git 仓库，则使用 Git 仓库地址
- 如果不是 Git 仓库，则回退到项目目录名
- 写入和搜索使用同一套 `project_name` 规则
- 搜索时只返回当前 `project_name` 对应的记忆

## 数据模型与分层

数据库操作全部集中在 `internal/models`：

- `internal/models/store.go`：数据库打开、迁移、事务、驱动选择
- `internal/models/memory.go`：`memories` 表
- `internal/models/memory_embedding.go`：`memory_embeddings` 表
- `internal/models/memory_metadata.go`：`memory_metadata` 表
- `internal/models/user.go`：`users` 表

约束：

- 业务层只能调用 `models` 暴露的方法查询数据
- 不对外暴露 `sql.DB`
- 不对外暴露 `gorm.DB`

## 服务端接口

### `GET /healthz`

健康检查。

### 管理端 JWT 接口

- 路由前缀：`/api/v1/users`
- 请求头：`Authorization: Bearer <jwt>`

#### `POST /api/v1/users/auth/login`

```json
{
  "username": "admin",
  "password": "随机密码"
}
```

#### `GET /api/v1/users/me`

返回当前登录用户。

#### `GET /api/v1/users`

返回用户列表。

#### `POST /api/v1/users`

```json
{
  "username": "alice",
  "real_name": "Alice Zhang",
  "password": "secret123",
  "is_admin": false
}
```

#### `PUT /api/v1/users/:id`

```json
{
  "username": "alice",
  "real_name": "Alice Zhang",
  "password": "",
  "is_admin": false,
  "regenerate_token": true
}
```

#### `DELETE /api/v1/users/:id`

删除指定用户，默认不允许删除当前登录用户。

### 记忆 API Token 接口

- 路由前缀：`/tokenapi/v1`
- 请求头：`X-API-Token: <api-token>`

#### `POST /tokenapi/v1/memories/search`

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

#### `POST /tokenapi/v1/memories/write`

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

写入时服务端会：

- 根据 `X-API-Token` 识别调用用户
- 把用户的 `userid(UUID)` 写入记忆记录
- 把 `user_id` 写入记忆正文头部，便于追溯来源

## 嵌入与语义检索

当 `embedding.base_url` 和 `embedding.model` 配置完整时，会启用语义检索；`api_key` 可为空，以兼容本地无鉴权服务。

启用后行为：

- 写入时同步写入向量
- 搜索时在关键字命中外追加语义召回
- 服务启动时检查当前模型与数据库记录是否一致
- 模型切换时自动重建 `memory_embeddings`
- 向量记录继续通过 `project_name` 做隔离

兼容的接口形式：

```text
POST <base_url>/embeddings
```

## 前端界面

服务端编译时会把 `web/dist` 通过 `go embed` 嵌入二进制。

当前界面包含：

- 登录页
- 管理首页
- 用户管理页
- JWT 路由守卫
- 用户新增、编辑、删除、重置 API Token

注意：如果直接运行 `go test ./...` 或构建服务端，而 `web/dist` 尚未生成，`go embed` 会因为找不到静态资源而失败。先执行：

```bash
make -f Markfile build-ui
```

或完整构建：

```bash
make -f Markfile build
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

常用命令：

```bash
make -f Markfile deps
make -f Markfile build-ui
make -f Markfile build
make -f Markfile test
make -f Markfile release
```

运行后端相关测试：

```bash
go test ./internal/memory ./internal/user ./internal/models ./internal/config
```

运行完整 Go 测试前，请先生成前端资源：

```bash
make -f Markfile build-ui
go test ./...
```

运行 Python 测试：

```bash
python3 -m unittest hive/scripts/test_main.py
```

## 常见问题

### 为什么不同项目会共用一个数据库？

- 这是当前版本的设计目标
- 服务端只维护一份数据库
- 项目隔离依赖 `project_name`，而不是数据库文件路径

### 为什么搜索结果为空？

- 检查写入和搜索时使用的 `project_name` 是否一致
- 检查客户端 `alias` / `project_alias` 是否发生变化
- 检查请求头里是否带了有效的 `X-API-Token`
- 检查服务端是否连接到了你预期的数据库

### 为什么 `go test ./...` 会失败？

- 因为 `web/embed.go` 依赖 `web/dist/*`
- 如果前端资源尚未构建，`go embed` 会报错
- 先执行 `make -f Markfile build-ui`

### 为什么服务端没有读取 `~/.config/hive/config.json`？

- 因为服务端只读取当前工作目录下的 `./config.json`
- `~/.config/hive/config.json` 仅作为客户端配置使用

## 仓库信息

- Go module：`github.com/nzlov/hive`
- 服务端入口：`cmd/hive-server`
- 客户端入口：`hive/scripts/main.py`
- 前端入口：`web/src/main.js`
