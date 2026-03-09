SHELL := /bin/bash

BINARY := bin/hive-server

.PHONY: help deps deps-ui build-ui build-server build test dev-server dev-ui dev clean release

# help 统一展示常用命令，避免团队成员需要先阅读脚本内容才知道入口。
help:
	@printf "可用命令:\n"
	@printf "  make deps          # 安装前后端依赖\n"
	@printf "  make build-ui      # 构建 Vue 前端\n"
	@printf "  make build         # 构建前端并编译 Go 服务\n"
	@printf "  make test          # 运行 Go 测试\n"
	@printf "  make dev-server    # 启动 Go 服务\n"
	@printf "  make dev-ui        # 启动 Vue 开发服务\n"
	@printf "  make dev           # 同时启动前后端开发服务\n"
	@printf "  make clean         # 清理构建产物\n"
	@printf "  make release       # 构建可发布产物\n"

# deps 统一安装项目依赖，避免打包前遗漏前端开发依赖或 Go 模块更新。
deps: deps-ui
	go mod tidy

# deps-ui 只处理前端依赖安装，避免每次执行前端构建都重复下载包。
deps-ui:
	npm install --include=dev --prefix web

# build-ui 先构建前端资源，确保 go embed 在编译时总能拿到最新 dist 内容。
build-ui:
	npm run build --prefix web

# build-server 在嵌入资源就绪后编译服务端，避免发布包缺失管理后台页面。
build-server:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/hive-server

# build 统一完成前端构建和服务编译，减少手工发布时的顺序错误。
build: build-ui build-server

# test 保持后端回归检查独立可执行，便于修改鉴权和记忆逻辑后快速验证。
test:
	go test ./...

# dev-server 直接启动 Go 服务，便于配合独立前端开发服务器联调。
dev-server:
	go run ./cmd/hive-server

# dev-ui 启动 Vue 开发服务，利用 Vite 代理把 API 请求转发到 Go 服务。
dev-ui:
	npm run dev --prefix web -- --host 0.0.0.0

# dev 并行启动前后端开发服务，降低本地联调时手工开多个终端的成本。
dev:
	$(MAKE) -f Markfile dev-server & $(MAKE) -f Markfile dev-ui

# clean 清理构建输出，避免旧二进制和旧前端产物干扰新的打包结果。
clean:
	rm -rf bin web/dist

# release 生成前端静态资源、二进制和后端测试结果，便于发布前做一次完整收敛。
release: build test
