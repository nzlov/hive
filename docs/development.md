# 开发与测试

## 常用命令

```bash
make deps
make build-ui
make build-server
make build
make dev-server
make dev-ui
make dev
make test
make release
make clean
```

## 构建与运行说明

- `make deps`：安装前端依赖并执行 `go mod tidy`
- `make build-ui`：构建前端静态资源到 `web/dist`
- `make build-server`：在前端资源就绪后编译 `bin/hive-server`
- `make build`：完整构建前端和服务端发布产物
- `make dev-server`：只启动 Go 服务端
- `make dev-ui`：只启动前端开发服务器
- `make dev`：同时启动前后端开发服务
- `make release`：先清理旧产物，再构建并运行完整测试

## 配置与数据说明

- 首次启动时会在当前目录自动生成默认 `config.json`
- `database.driver` 为空或缺失时，默认回落到 `SQLite`
- `SQLite` 默认使用 `./.memory/memory.db`
- `PostgreSQL` 需要显式设置 `database.driver=postgresql` 和 `database.dsn`
- 自动生成配置后，可按需编辑当前目录下的 `config.json`

## 本地访问与联调

- 服务默认地址：`http://127.0.0.1:8080`
- 前端开发服务器默认地址：`http://127.0.0.1:5173`
- Vite 代理会把 `/api/*` 和 `/tokenapi/*` 转发到 `http://127.0.0.1:8080`

## 默认管理员说明

首次启动如果数据库里没有任何用户，服务端会自动创建默认管理员：

- 用户名：`admin`
- 真实名称：`系统管理员`
- `userid`：自动生成 `UUID`
- 密码：随机生成，并打印在启动日志中
- `apitoken`：自动生成，可在管理后台查看

## 运行后端相关测试

```bash
go test ./internal/memory ./internal/user ./internal/models ./internal/config
```

## 运行完整 Go 测试

运行完整 Go 测试前，请先生成前端资源：

```bash
make build-ui
go test ./...
```

也可以直接执行：

```bash
make test
```

## 清理说明

```bash
make clean
```

`make clean` 会删除以下内容，请在执行前确认本地数据是否需要保留：

- `bin`
- `web/dist`
- `.memory`
- `config.json`

## 清理治理入口

- 管理员登录后可在后台侧边栏进入“清理治理”页面
- 页面支持保护标签增删改、手动生成待审核候选、批量批准/拒绝、只对勾选且已批准的候选执行删除

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
