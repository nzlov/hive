SHELL := /bin/bash
.DEFAULT_GOAL := help

GO ?= go
NPM ?= npm

BINARY := bin/hive-server
WEB_DIR := web
WEB_NODE_MODULES_STAMP := $(WEB_DIR)/node_modules/.package-lock.stamp

.PHONY: help deps deps-ui build-ui build-server build test dev-server dev-ui dev clean release

# help 统一展示常用命令，避免团队成员需要先阅读脚本内容才知道入口。
help:
	@printf "可用命令:\n"
	@printf "  make deps          # 安装前后端依赖\n"
	@printf "  make build-ui      # 构建 Vue 前端\n"
	@printf "  make build-server  # 编译 Go 服务\n"
	@printf "  make build         # 构建前端并编译 Go 服务\n"
	@printf "  make test          # 运行 Go 测试\n"
	@printf "  make dev-server    # 启动 Go 服务\n"
	@printf "  make dev-ui        # 启动 Vue 开发服务\n"
	@printf "  make dev           # 同时启动前后端开发服务\n"
	@printf "  make clean         # 清理构建产物\n"
	@printf "  make release       # 构建可发布产物\n"

# deps 统一安装项目依赖，避免打包前遗漏前端开发依赖或 Go 模块更新。
deps: deps-ui
	$(GO) mod tidy


# WEB_NODE_MODULES_STAMP 用锁文件驱动依赖安装，避免重复执行 npm install 拖慢本地开发反馈。
$(WEB_NODE_MODULES_STAMP): $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	$(NPM) install --include=dev --prefix $(WEB_DIR)
	@mkdir -p $(dir $@)
	@touch $@

# deps-ui 只处理前端依赖安装，避免每次执行前端构建都重复下载包。
deps-ui: $(WEB_NODE_MODULES_STAMP)

# build-ui 先补齐前端依赖再构建资源，避免在全新环境里直接执行构建失败。
build-ui: deps-ui
	$(NPM) run build --prefix $(WEB_DIR)

# build-server 依赖最新前端产物，避免直接编译服务端时漏掉 go:embed 需要的静态资源。
build-server: build-ui
	mkdir -p bin
	$(GO) build -o $(BINARY) ./cmd/hive-server

# build 统一完成前端构建和服务编译，减少手工发布时的顺序错误。
build: build-server

# test 先准备前端产物，避免 clean 后的 go test 因缺少嵌入资源直接失败。
test: build-ui
	$(GO) test ./...

# dev-server 直接启动 Go 服务，便于配合独立前端开发服务器联调。
dev-server:
	$(GO) run ./cmd/hive-server

# dev-ui 启动前会补齐前端依赖，避免新环境首次联调时因为缺包而中断。
dev-ui: deps-ui
	$(NPM) run dev --prefix $(WEB_DIR) -- --host 0.0.0.0

# dev 用 wait 和信号清理子进程，避免任一服务退出后留下孤儿进程。
dev:
	@server_pid=; ui_pid=; \
	cleanup() { \
		if [ -n "$$server_pid" ]; then kill "$$server_pid" 2>/dev/null || true; fi; \
		if [ -n "$$ui_pid" ]; then kill "$$ui_pid" 2>/dev/null || true; fi; \
	}; \
	trap 'cleanup' INT TERM EXIT; \
	$(MAKE) dev-server & server_pid=$$!; \
	$(MAKE) dev-ui & ui_pid=$$!; \
	wait -n "$$server_pid" "$$ui_pid"; \
	status=$$?; \
	cleanup; \
	wait "$$server_pid" 2>/dev/null || true; \
	wait "$$ui_pid" 2>/dev/null || true; \
	exit $$status

# clean 清理构建输出，避免旧二进制和旧前端产物干扰新的打包结果。
clean:
	rm -rf bin $(WEB_DIR)/dist .memory config.json

# release 先清理旧产物再完整构建和测试，避免把历史残留误当成当前发布结果。
release: clean build test
