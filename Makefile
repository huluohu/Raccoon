# Raccoon 开发 Makefile
GO      ?= go
NODE    ?= node
NPM     ?= npm
VERSION ?= 0.1.0
LDFLAGS = -s -w -X github.com/huluohu/raccoon/internal/server.version=$(VERSION)
# -buildvcs=false：部分沙箱/容器内 Go 的 VCS 戳采集会失败（git 归属权检查），构建产物不需要 VCS 信息
GOBUILD = $(GO) build -buildvcs=false
GOTEST  = $(GO) test -buildvcs=false

.PHONY: help backend-build backend-run backend-test web-install web-dev web-build verify docker-build fmt vet

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n", $$1, $$2}'

backend-build: ## 编译后端（产物 bin/raccoon）
	cd backend && $(GOBUILD) -ldflags "$(LDFLAGS)" -o ../bin/raccoon ./cmd/raccoon

backend-run: backend-build ## 本地运行（数据目录 ./data，监听 :8530）
	RACCOON_DATA_DIR=./data RACCOON_SERVER_ADDR=:8530 ./bin/raccoon

backend-test: ## 后端测试
	cd backend && $(GOTEST) ./...

web-install: ## 安装前端依赖
	cd web && $(NPM) install

web-dev: ## 前端开发服务器 :5173
	cd web && $(NPM) run dev

web-build: ## 前端构建并拷贝到 backend/internal/web/dist（供 embed）
	cd web && $(NPM) run build
	rm -rf ../backend/internal/web/dist
	cp -r dist ../backend/internal/web/dist

verify: fmt vet backend-build web-build ## 一键验证：fmt/vet/编译/前端类型检查与构建
	@echo "== verify 完成 =="

fmt: ## gofmt 检查
	cd backend && test -z "$$($(GO)fmt -l .)" || (echo "gofmt 未通过：" && $(GO)fmt -l . && exit 1)

vet: ## go vet
	cd backend && $(GO) vet ./...

docker-build: ## 构建镜像（amd64/arm64 由 --platform 指定）
	docker build -t raccoon:$(VERSION) .
