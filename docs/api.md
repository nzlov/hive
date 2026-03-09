# 服务端接口

## `GET /healthz`

健康检查。

## 管理端 JWT 接口

- 路由前缀：`/api/v1/users`
- 请求头：`Authorization: Bearer <jwt>`

### `POST /api/v1/users/auth/login`

```json
{
  "username": "admin",
  "password": "随机密码"
}
```

### `GET /api/v1/users/me`

返回当前登录用户。

### `GET /api/v1/users`

返回用户列表。

### `POST /api/v1/users`

```json
{
  "username": "alice",
  "real_name": "Alice Zhang",
  "password": "secret123",
  "is_admin": false
}
```

### `PUT /api/v1/users/:id`

```json
{
  "username": "alice",
  "real_name": "Alice Zhang",
  "password": "",
  "is_admin": false,
  "regenerate_token": true
}
```

### `DELETE /api/v1/users/:id`

删除指定用户，默认不允许删除当前登录用户。

## 记忆 API Token 接口

- 路由前缀：`/tokenapi/v1`
- 请求头：`X-API-Token: <api-token>`

### `POST /tokenapi/v1/memories/search`

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

### `POST /tokenapi/v1/memories/write`

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
