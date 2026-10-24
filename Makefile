# Raccoon 开发 Makefile
GO      ?= go
NODE    ?= node
NPM     ?= npm
VERSION ?= 1.0.0
LDFLAGS = -s -w -X github.com/huluohu/raccoon/internal/server.version=$(VERSION)
# -buildvcs=false：部分沙箱/容器内 Go 的 VCS 戳采集会失败（git 归属权检查），构建产物不需要 VCS 信息
GOBUILD = $(GO) build -buildvcs=false
GOTEST  = $(GO) test -buildvcs=false

.PHONY: help backend-build cli-build backend-run backend-test web-install web-dev web-build verify docker-build docker-release fmt vet

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n", $$1, $$2}'

backend-build: ## 编译后端（产物 bin/raccoon）
	cd backend && $(GOBUILD) -ldflags "$(LDFLAGS)" -o ../bin/raccoon ./cmd/raccoon

cli-build: ## 编译 CLI（产物 bin/rc；--json/--jsonl 供脚本与 AI Agent 管道使用）
	cd backend && $(GOBUILD) -ldflags "$(LDFLAGS)" -o ../bin/rc ./cmd/rc

backend-run: backend-build ## 本地运行（数据目录 ./data，监听 :8530）
	RACCOON_DATA_DIR=./data RACCOON_SERVER_ADDR=:8530 ./bin/raccoon

backend-test: ## 后端测试
	cd backend && $(GOTEST) ./...

web-test: ## 前端测试（vitest）
	cd web && $(NPM) run test

web-install: ## 安装前端依赖
	cd web && $(NPM) install

web-dev: ## 前端开发服务器 :5173
	cd web && $(NPM) run dev

web-build: ## 前端构建并拷贝到 backend/internal/web/dist（供 embed）
	cd web && $(NPM) run build && rm -rf ../backend/internal/web/dist && cp -r dist ../backend/internal/web/dist

verify: fmt vet backend-build web-build design-check ## 一键验证：fmt/vet/编译/前端类型检查与构建/设计方案符合度
	@echo "== verify 完成 =="

design-check: ## 设计方案符合度对账（docs/07 §2 Token / §3 路由 / §4 组件 / §5 表情 / §7 交互）
	@python3 scripts/check-design-conformance.py

fmt: ## gofmt 检查
	cd backend && test -z "$$($(GO)fmt -l .)" || (echo "gofmt 未通过：" && $(GO)fmt -l . && exit 1)

vet: ## go vet
	cd backend && $(GO) vet ./...

docker-build: ## 构建镜像（amd64/arm64 由 --platform 指定）
	docker build --build-arg VERSION=$(VERSION) -t raccoon-server:$(VERSION) .

HUB_NS ?= fooololo          # Docker Hub 命名空间（docker login 的账号）
docker-release: ## 构建多架构镜像并推 Docker Hub（make docker-release VERSION=1.0.0）
	docker buildx build --platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(HUB_NS)/raccoon-server:$(VERSION) -t $(HUB_NS)/raccoon-server:latest --push .
