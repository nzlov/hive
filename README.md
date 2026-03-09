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

```
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
├── docs/                     # 详细文档
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

详见 [docs/getting-started.md](./docs/getting-started.md)

## 文档索引

- [快速开始](./docs/getting-started.md)
- [服务端配置](./docs/server-config.md)
- [Hive Skill 使用](./docs/hive-skill.md)
- [服务端接口](./docs/api.md)
- [嵌入与语义检索](./docs/embedding.md)
- [前端界面](./docs/frontend.md)
- [开发与测试](./docs/development.md)
- [常见问题](./docs/faq.md)

## 仓库信息

- Go module：`github.com/nzlov/hive`
- 服务端入口：`cmd/hive-server`
- 客户端入口：`hive/scripts/main.py`
- 前端入口：`web/src/main.js`
