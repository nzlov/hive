# 前端界面

服务端编译时会把 `web/dist` 通过 `go embed` 嵌入二进制。

## 当前界面包含

- 登录页
- 管理首页
- 用户管理页
- JWT 路由守卫
- 用户新增、编辑、删除、重置 API Token

## 构建注意事项

如果直接运行 `go test ./...` 或构建服务端，而 `web/dist` 尚未生成，`go embed` 会因为找不到静态资源而失败。先执行：

```bash
make build-ui
```

或完整构建：

```bash
make build
```
