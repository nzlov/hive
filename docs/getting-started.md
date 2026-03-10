# 快速开始

## 构建二进制

```bash
make build
```

构建完成后会生成服务端二进制：`bin/hive-server`

## 启动服务

```bash
./bin/hive-server
```

首次启动时，程序会自动生成默认 `config.json`、初始化本地 `.memory` 数据目录，并在没有用户时创建默认管理员。

## 访问服务

- 管理后台：`http://127.0.0.1:8080`
- 健康检查：`http://127.0.0.1:8080/healthz`
- Token API 前缀：`http://127.0.0.1:8080/tokenapi/v1`

默认管理员信息会打印在启动日志中，包括：

- 用户名：`admin`
- 真实名称：`系统管理员`
- `userid`：自动生成 `UUID`
- 密码：随机生成
- `apitoken`：自动生成，可在管理后台查看

## 使用方式

浏览器访问管理后台后，可以直接登录并管理记忆、用户与清理治理。

```bash
curl http://127.0.0.1:8080/healthz
```

也可以通过客户端脚本访问服务：

```bash
python3 hive/scripts/main.py search --root . --query "向量重建"
```

详细的开发、测试、清理与调试说明见 `docs/development.md`。
