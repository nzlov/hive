# 开发与测试

## 常用命令

```bash
make deps
make build-ui
make build
make test
make release
```

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
