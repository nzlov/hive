# Repository Agent Guide

## 项目概览

- 仓库名：`hive`，Go module 为 `github.com/nzlov/hive`。
- 这是一个多语言仓库：Go HTTP 服务端 + Python CLI/skill 脚本 + Vue 3 管理后台。
- 后端入口在 `cmd/hive-server`，Python 客户端入口在 `hive/scripts/main.py`，前端入口在 `web/src/main.js`。
- 数据访问统一收口在 `internal/models`，业务层不应直接暴露或依赖 `sql.DB` / `gorm.DB`。
- 前端构建产物会被服务端嵌入；因此很多 Go 构建或测试动作依赖先生成 `web/dist`。

## 目录速览

- `cmd/hive-server`：服务端启动入口与依赖装配。
- `internal/api`：HTTP 请求/响应结构体定义。
- `internal/config`：配置加载、默认值与规范化。
- `internal/memory`：搜索、写入、向量、清理等核心业务逻辑。
- `internal/models`：GORM 模型、迁移、数据库封装。
- `internal/server`：Gin 路由、中间件、接口测试。
- `internal/user`：用户、JWT、API Token 相关逻辑。
- `hive/scripts`：Python CLI，负责和服务端 HTTP 协议交互。
- `web/src`：Vue 管理后台页面、路由、API 封装与样式。

## 规则文件状态

- 未发现仓库级 `/.cursorrules`。
- 未发现 `/.cursor/rules/` 目录。
- 未发现 `/.github/copilot-instructions.md`。
- 因此当前文件应视为此仓库给 agent 的主规范文档。

## 环境要求

- Go `1.25+`
- Node.js `20+`
- Python `3.10+`
- 默认本地服务端地址：`http://127.0.0.1:8080`
- 默认前端开发地址：`http://127.0.0.1:5173`

## 安装与依赖命令

- 安装全部依赖：`make deps`
- 仅安装前端依赖：`make deps-ui`
- `make deps` 会执行 `go mod tidy`
- 前端依赖安装由 `web/package-lock.json` 驱动，命令内部使用 `npm install --include=dev --prefix web`

## 构建命令

- 构建前端：`make build-ui`
- 构建后端：`make build-server`
- 完整构建：`make build`
- 生成可发布产物并跑测试：`make release`
- 后端二进制输出：`bin/hive-server`

## 开发命令

- 启动 Go 服务：`make dev-server`
- 启动前端开发服务器：`make dev-ui`
- 同时启动前后端：`make dev`
- 直接运行后端入口也可使用：`go run ./cmd/hive-server`
- 直接运行前端开发服务器也可使用：`npm run dev --prefix web -- --host 0.0.0.0`

## 测试命令

- 完整 Go 测试：`make test`
- `make test` 会先执行 `make build-ui`，再运行 `go test ./...`
- 若你直接执行 Go 测试，先运行：`make build-ui`
- 然后再运行：`go test ./...`
- 后端核心包测试示例：`go test ./internal/memory ./internal/user ./internal/models ./internal/config`
- Python 脚本测试：`python3 -m unittest hive.scripts.test_main`

## 单个测试运行

- 运行单个 Go 包中的某个测试：`go test ./internal/server -run TestRouterWriteAndSearch`
- 运行同一包内匹配多个测试：`go test ./internal/server -run 'TestRouter.*'`
- 运行单个 Python unittest：`python3 -m unittest hive.scripts.test_main.BranchFilterTest.test_filter_hits_by_branch`
- 若某个 Go 单测依赖嵌入前端资源，先执行：`make build-ui`
- 当前仓库没有配置前端自动化测试命令；不要虚构 `npm test`

## Lint / 格式化现状

- 仓库当前没有 `make lint`、`golangci-lint`、`eslint`、`prettier` 或 `npm run lint` 脚本。
- 前端 `package.json` 只定义了 `dev`、`build`、`preview`。
- 因此修改后最可靠的验证组合是：`make build-ui` + `go test ./...` + `python3 -m unittest hive.scripts.test_main`
- Go 代码默认遵循 `gofmt` 风格；提交前应至少运行 `gofmt` 于变更的 `.go` 文件。
- 若调整前端样式或构建链路，至少运行：`npm run build --prefix web`

## 清理命令

- 清理构建产物与本地数据：`make clean`
- `make clean` 会删除：`bin`、`web/dist`、`.memory`、`config.json`
- 这是破坏性命令；除非任务明确需要，不要自动执行

## 架构与分层约束

- 保持单一职责：每个 package 只解决一个问题。
- 严禁循环依赖。
- 数据库相关逻辑只放在 `internal/models`。
- 业务层通过 `models.StoreFromContext` 获取存储，不要在服务层暴露底层连接。
- HTTP 层应尽量薄；协议结构放在 `internal/api`，业务逻辑放在 `internal/memory` 或 `internal/user`。
- 启动装配逻辑集中在 `cmd/hive-server/main.go`，不要把复杂业务塞进入口函数之外的无关位置。

## Go 代码风格

- 始终使用 `gofmt` 风格，保持 tab 缩进与标准导入分组。
- import 顺序遵循 Go 默认约定：标准库、第三方、项目内包，组间空一行。
- 优先使用小而明确的结构体和函数，延续当前仓库“配置/模型/服务”分层方式。
- 导出符号必须有中文注释；未导出但承载主要逻辑的函数通常也已有中文注释，新代码应保持一致。
- 注释要解释“为什么这样做”，不要只复述“做了什么”。
- 错误信息使用中文，且尽量面向调用方表达，例如“搜索描述不能为空”。
- 对外暴露的错误语义应稳定；若底层错误需要转换，优先在模型层或服务层统一归一。
- 倾向尽早返回错误，避免深层嵌套。
- 时间处理统一偏向 `time.Now().UTC()` 和 `time.RFC3339Nano`。
- 字符串输入通常先 `strings.TrimSpace`；新增入口参数处理时延续此习惯。
- 分页、标签、项目名等规范化逻辑优先抽成辅助函数，不要在 handler 中重复散写。
- 并发搜索逻辑当前使用 `errgroup`、`singleflight`；新增并发流程时保持可取消、可限流、可读。

## Go 类型与命名

- 包名保持短小、全小写、无下划线。
- 导出类型/函数使用 PascalCase，未导出符号使用 camelCase。
- 错误变量采用 `errXxx` 命名，例如 `errInvalidProjectMergeInput`。
- 请求/响应 DTO 放在 `internal/api`，名称使用 `XxxRequest` / `XxxResponse` / `XxxItem`。
- 服务对象统一命名为 `Service`，通过 `NewService(...)` 构造。
- 配置对象统一集中在 `AppConfig` 及其子结构中，不要引入零散全局配置读取。

## Python 代码风格

- 使用 Python 3 类型标注，当前代码普遍采用 `dict[str, Any]`、`list[str]` 等现代写法。
- 维持 `pathlib.Path` 优先于裸字符串路径操作。
- CLI 参数解析统一收敛在 `argparse`，不要随意引入新的命令行框架。
- 错误退出通常用 `SystemExit`，与现有脚本风格保持一致。
- 测试使用标准库 `unittest`，新增脚本测试优先沿用，不要混入 `pytest` 风格。
- 文档字符串与注释使用中文，说明设计原因与兼容性考虑。

## 前端代码风格

- 前端使用 Vue 3 单文件组件，偏向 `<script setup>`。
- JavaScript 使用 ES module；当前未使用 TypeScript，不要随意混入 `.ts`。
- import 保持简洁稳定：第三方依赖在前，本地模块在后；同类相邻组织。
- API 请求统一走 `web/src/lib/api.js` 的 `request()` 包装，不要在页面中直接复制 token/header 处理。
- 路由守卫统一放在 `web/src/router/index.js`，不要在每个页面重复做鉴权跳转。
- 样式主要依赖 Tailwind utility 与 `web/src/styles.css` 中的组件类；优先复用 `panel`、`field`、`primary-btn` 等现有语义类。
- 视觉体系已定义颜色变量语义：`ink`、`mist`、`brass`、`pine`、`coral`；新增页面优先沿用这套命名。
- 表单与异步交互遵循当前模式：`loading` / `errorMessage` / `actionMessage` 之类的显式状态字段。
- 页面文案以中文为主，少量品牌或模块标题允许英文点缀。
- 若修改前端，确保桌面与移动端都能正常加载，且 `npm run build --prefix web` 成功。

## 命名与数据约定

- JSON 字段普遍采用 snake_case，如 `project_name`、`git_branch`、`page_size`。
- Go 结构体字段使用 PascalCase，并通过 struct tag 映射到 snake_case JSON。
- 前端响应对象保持后端字段名，不要私自重命名协议字段。
- `summary` / `error` 是核心记忆类型常量，新增逻辑时不要写出第三种隐式类型。
- 搜索协议使用 `tags + description`；不要回退到旧 `queries` 协议。

## 错误处理与安全

- 优先返回结构化错误，不要让调用方解析非结构化文本。
- 管理端 JWT 与 Token API 是两条不同鉴权链路；不要混用接口前缀。
- 后端默认管理员会在空库时自动创建；涉及该流程的改动需特别谨慎。
- 记录或展示数据库连接信息时应脱敏，参考 `buildPostgresLabel` 的处理方式。
- 删除、清理、合并项目/标签这类操作可能影响向量与审核数据，修改时要同步考虑关联刷新与失效逻辑。

## Agent 工作建议

- 改 Go 代码前先确认该变更是否要求先构建前端资源。
- 改搜索、清理、路由协议时，优先补或更新对应包测试。
- 改 Python CLI 协议时，同时检查 `hive/scripts/test_main.py` 是否需要覆盖。
- 改前端 API 协议时，同时核对 `internal/api`、Gin 路由和 `web/src/lib/api.js`。
- 若没有新增 lint 工具，不要在文档或提交中声称已运行不存在的检查。
