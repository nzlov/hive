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

## 管理端记忆治理接口

- 路由前缀：`/api/v1/admin/memories`
- 请求头：`Authorization: Bearer <jwt>`
- 权限：仅管理员

### `GET /api/v1/admin/memories/protected-tags`

返回当前数据库中的保护标签列表。

### `POST /api/v1/admin/memories/protected-tags`

```json
{
  "tag": "核心故障",
  "description": "命中后永不进入清理候选",
  "enabled": true
}
```

### `PUT /api/v1/admin/memories/protected-tags/:id`

```json
{
  "tag": "架构决策",
  "description": "长期保留的设计决策",
  "enabled": true
}
```

### `DELETE /api/v1/admin/memories/protected-tags/:id`

删除指定保护标签。

### `GET /api/v1/admin/memories/cleanup-reviews`

支持查询参数：

- `page`
- `page_size`
- `status`
- `type`
- `project_name`

返回待审核、已批准、已拒绝或已执行的清理记录。

### `POST /api/v1/admin/memories/cleanup-reviews/run`

手动生成一轮待审核候选。

### `POST /api/v1/admin/memories/cleanup-reviews/approve`

```json
{
  "ids": [1, 2, 3]
}
```

批量批准候选。

### `POST /api/v1/admin/memories/cleanup-reviews/reject`

```json
{
  "ids": [4, 5]
}
```

批量拒绝候选。

### `POST /api/v1/admin/memories/cleanup-reviews/execute`

```json
{
  "ids": [1, 2]
}
```

只执行当前请求里勾选且已经处于 `approved` 状态的审核记录。

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

搜索结果真正返回命中时，服务端会同步更新对应记忆的 `use_count` 和 `last_used_at`。

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
      "context": "# internal/pay/retry.go\n\n## 支付超时故障\n\n- 错误现象: 高峰期支付请求大量超时。\n- 触发条件: 重试队列积压且下游响应变慢。\n- 根因: 连接池耗尽导致请求持续排队。\n- 修复动作: 限制重试并扩容连接池。\n- 修复结论: 超时链路恢复稳定。\n- 验证结果: 错误率从 12% 降至 0.3%。"
    }
  ]
}
```

写入时服务端会：

- 根据 `X-API-Token` 识别调用用户
- 把用户的 `userid(UUID)` 写入记忆记录
- 推荐 `summary` 记忆正文尽量稳定使用 `详情 / 结论 / 约束 / 依赖`
- 推荐 `error` 记忆正文尽量稳定使用 `错误现象 / 触发条件 / 根因 / 修复动作 / 修复结论 / 验证结果`
