# 常见问题

## 为什么不同项目会共用一个数据库？

- 这是当前版本的设计目标
- 服务端只维护一份数据库
- 项目隔离依赖 `project_name`，而不是数据库文件路径

## 为什么搜索结果为空？

- 检查写入和搜索时使用的 `project_name` 是否一致
- 检查客户端 `alias` / `project_alias` 是否发生变化
- 检查请求头里是否带了有效的 `X-API-Token`
- 检查服务端是否连接到了你预期的数据库

## 为什么 `go test ./...` 会失败？

- 因为 `web/embed.go` 依赖 `web/dist/*`
- 如果前端资源尚未构建，`go embed` 会报错
- 先执行 `make build-ui`

## 为什么服务端没有读取 `~/.config/hive/config.json`？

- 因为服务端只读取当前工作目录下的 `./config.json`
- `~/.config/hive/config.json` 仅作为客户端配置使用
