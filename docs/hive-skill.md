# Hive Skill 使用说明

`hive` 是一个可安装到 AI 开发工具 `skills` 目录中的 Skill，用来提供 Hive 记忆能力。

## 放在什么位置

应把整个 `hive` 目录放到 AI 开发工具约定的 `skills` 目录中，而不是项目业务目录中。

推荐目录结构：

```text
<AI工具配置目录>/
  skills/
    hive/
      SKILL.md
      scripts/
      agents/
```

放置要求：

- `hive` 目录应作为一个完整 Skill 放在 `skills` 目录下
- `SKILL.md` 必须保留在 `skills/hive/SKILL.md`，这是 Skill 入口文件
- `scripts/`、`agents/` 需要一起保留，不要只复制单个脚本文件

以 OpenCode 为例，默认位置类似：

```text
~/.config/opencode/skills/hive/
```

## 目录作用

- `skills/hive/SKILL.md`：定义这个 Skill 的说明和使用规则
- `skills/hive/scripts/main.py`：Hive 客户端脚本入口
- `skills/hive/agents/`：Skill 依赖的代理配置

## 如何使用这个 Skill

当 AI 开发工具的 `skills/hive/SKILL.md` 存在时，工具就可以把它识别为一个可用 Skill。

使用约定：

- 把整个 `hive` 目录安装到工具的 `skills` 目录中
- 让工具通过 `SKILL.md` 加载 Skill 说明
- 如果需要手动执行脚本，使用 Skill 目录下的 `scripts/main.py`

## 如何配置

Hive 客户端读取本地配置文件：

```text
~/.config/hive/config.json
```

文件不存在时，脚本会自动创建最小配置骨架：

```json
{
  "defaultServerBaseUrl": "http://127.0.0.1:8080",
  "apiToken": "",
  "projects": {}
}
```

## 配置示例

```json
{
  "defaultServerBaseUrl": "http://127.0.0.1:8080",
  "apiToken": "global-token",
  "projects": {
    "/home/dev/work/repo-a": {
      "projectName": "team/repo-a",
      "baseUrl": "http://127.0.0.1:8080",
      "apiToken": "repo-a-token"
    },
    "github.com/acme/repo-b.git": {
      "projectName": "acme/repo-b",
      "baseUrl": "https://hive.example.com",
      "apiToken": "repo-b-token"
    }
  }
}
```

## 配置项说明

- `defaultServerBaseUrl`：默认服务地址，项目没有单独配置时使用
- `apiToken`：默认鉴权令牌，项目没有单独配置时使用
- `projects`：项目级配置，用来给不同仓库覆盖地址、令牌和项目名
- `projectName`：写入 Hive 时使用的项目标识
- `baseUrl`：当前项目覆盖默认服务地址

## `projects` 如何匹配

脚本会根据 `--root` 对应的项目路径匹配 `projects` 中的键，优先顺序如下：

1. 项目绝对路径
2. Git 远端仓库标识
3. 目录名

推荐做法：

- 优先使用项目绝对路径作为键
- 多个仓库共用同一服务时，也分别写独立项目配置
- 需要鉴权时，优先在项目级单独配置 `apiToken`

## 最小接入步骤

1. 把完整的 `hive` 目录放到 AI 工具的 `skills` 目录
2. 确认 `skills/hive/SKILL.md` 存在
3. 配置 `~/.config/hive/config.json`
4. 让 AI 工具从 `skills/hive/` 加载这个 Skill
