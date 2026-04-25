# Makefile for e-cam-service
# Go 项目构建和开发工具

# 项目配置
PROJECT_NAME := e-cam-service
BINARY_NAME := e-cam-service
MODULE_NAME := github.com/Havens-blog/e-cam-service
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建标志
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.CommitHash=$(COMMIT_HASH)"

# Go 相关配置
GO := go
GOCMD := $(GO)
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOLINT := golangci-lint

# 目录配置
BUILD_DIR := build
DIST_DIR := dist
LOGS_DIR := logs

# 平台配置
PLATFORMS := windows/amd64 linux/amd64 darwin/amd64 darwin/arm64

# 默认目标
.DEFAULT_GOAL := help

# 帮助信息
.PHONY: help
help: ## 显示帮助信息
	@echo "$(PROJECT_NAME) - 开发工具"
	@echo ""
	@echo "可用命令:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# 开发相关命令
.PHONY: dev
dev: wire ## 启动开发服务器
	@echo "🚀 启动开发服务器..."
	$(GOCMD) run main.go start

.PHONY: build
build: clean wire ## 构建应用程序
	@echo "🔨 构建应用程序..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "✅ 构建完成: $(BUILD_DIR)/$(BINARY_NAME)"

.PHONY: build-all
build-all: clean wire ## 构建所有平台的二进制文件
	@echo "🔨 构建所有平台..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		OS=$$(echo $$platform | cut -d'/' -f1); \
		ARCH=$$(echo $$platform | cut -d'/' -f2); \
		OUTPUT_NAME=$(BINARY_NAME)-$$OS-$$ARCH; \
		if [ $$OS = "windows" ]; then OUTPUT_NAME=$$OUTPUT_NAME.exe; fi; \
		echo "构建 $$OS/$$ARCH..."; \
		GOOS=$$OS GOARCH=$$ARCH $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$$OUTPUT_NAME .; \
	done
	@echo "✅ 所有平台构建完成"

.PHONY: install
install: build ## 安装到 GOPATH/bin
	@echo "📦 安装应用程序..."
	$(GOCMD) install $(LDFLAGS) .
	@echo "✅ 安装完成"

# 代码质量
.PHONY: fmt
fmt: ## 格式化代码
	@echo "🎨 格式化代码..."
	$(GOFMT) -s -w .
	$(GOCMD) mod tidy
	@echo "✅ 代码格式化完成"

.PHONY: lint
lint: ## 运行代码检查
	@echo "🔍 运行代码检查..."
	@if command -v $(GOLINT) >/dev/null 2>&1; then \
		$(GOLINT) run ./...; \
	else \
		echo "⚠️  golangci-lint 未安装，跳过代码检查"; \
		echo "安装命令: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

.PHONY: vet
vet: ## 运行 go vet
	@echo "🔍 运行 go vet..."
	$(GOCMD) vet ./...
	@echo "✅ go vet 检查完成"

# 测试相关
.PHONY: test
test: ## 运行测试
	@echo "🧪 运行测试..."
	$(GOTEST) -v ./...

.PHONY: test-coverage
test-coverage: ## 运行测试并生成覆盖率报告
	@echo "🧪 运行测试覆盖率..."
	@mkdir -p $(BUILD_DIR)
	$(GOTEST) -v -coverprofile=$(BUILD_DIR)/coverage.out ./...
	$(GOCMD) tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@echo "✅ 覆盖率报告生成: $(BUILD_DIR)/coverage.html"

.PHONY: test-race
test-race: ## 运行竞态检测测试
	@echo "🧪 运行竞态检测测试..."
	$(GOTEST) -race -v ./...

.PHONY: benchmark
benchmark: ## 运行基准测试
	@echo "🏃 运行基准测试..."
	$(GOTEST) -bench=. -benchmem ./...

# 依赖管理
.PHONY: deps
deps: ## 下载依赖
	@echo "📦 下载依赖..."
	$(GOMOD) download
	$(GOMOD) tidy

.PHONY: deps-update
deps-update: ## 更新依赖
	@echo "🔄 更新依赖..."
	$(GOMOD) tidy
	$(GOGET) -u ./...
	$(GOMOD) tidy

.PHONY: deps-vendor
deps-vendor: ## 创建 vendor 目录
	@echo "📦 创建 vendor 目录..."
	$(GOMOD) vendor

# Wire 依赖注入
.PHONY: wire
wire: ## 生成 Wire 依赖注入代码
	@echo "🔌 生成 Wire 代码..."
	@if command -v wire >/dev/null 2>&1; then \
		wire gen ./ioc; \
		wire gen ./internal/endpoint; \
	else \
		echo "⚠️  wire 未安装，正在安装..."; \
		$(GOGET) github.com/google/wire/cmd/wire; \
		wire gen ./ioc; \
		wire gen ./internal/endpoint; \
	fi
	@echo "✅ Wire 代码生成完成"

# Docker 构建
.PHONY: docker
docker: ## 构建 Docker 镜像
	@echo "🐳 构建 Docker 镜像..."
	docker build -f deploy/Dockerfile -t $(PROJECT_NAME):$(VERSION) .
	@echo "✅ Docker 镜像构建完成: $(PROJECT_NAME):$(VERSION)"

# 数据库相关
.PHONY: db-up
db-up: ## 启动数据库服务 (需要 docker-compose)
	@echo "🗄️  启动数据库服务..."
	@if [ -f deploy/docker-compose.yml ]; then \
		docker-compose -f deploy/docker-compose.yml up -d mongodb redis; \
	else \
		echo "⚠️  deploy/docker-compose.yml 文件不存在"; \
	fi

.PHONY: db-down
db-down: ## 停止数据库服务
	@echo "🗄️  停止数据库服务..."
	@if [ -f deploy/docker-compose.yml ]; then \
		docker-compose -f deploy/docker-compose.yml down; \
	else \
		echo "⚠️  deploy/docker-compose.yml 文件不存在"; \
	fi

# 清理
.PHONY: clean
clean: ## 清理构建文件
	@echo "🧹 清理构建文件..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -rf $(DIST_DIR)
	@rm -f $(BINARY_NAME)
	@rm -f $(BINARY_NAME).exe
	@echo "✅ 清理完成"

.PHONY: clean-cache
clean-cache: ## 清理 Go 缓存
	@echo "🧹 清理 Go 缓存..."
	$(GOCMD) clean -cache -modcache -testcache
	@echo "✅ 缓存清理完成"

# 工具安装
.PHONY: tools
tools: ## 安装开发工具
	@echo "🔧 安装开发工具..."
	$(GOGET) github.com/google/wire/cmd/wire
	$(GOGET) github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GOGET) github.com/swaggo/swag/cmd/swag@latest
	@echo "✅ 开发工具安装完成"

# API 文档
.PHONY: swagger
swagger: ## 生成 Swagger API 文档
	@echo "📚 生成 Swagger 文档..."
	@if command -v swag >/dev/null 2>&1; then \
		swag init; \
	else \
		echo "⚠️  swag 未安装，正在安装..."; \
		$(GOGET) github.com/swaggo/swag/cmd/swag@latest; \
		swag init; \
	fi
	@echo "✅ Swagger 文档生成完成"

.PHONY: swagger-serve
swagger-serve: swagger dev ## 生成文档并启动服务器
	@echo "🌐 Swagger 文档可在 http://localhost:8000/swagger/index.html 访问"

# 项目初始化
.PHONY: init
init: tools deps wire ## 初始化项目
	@echo "🚀 初始化项目..."
	@mkdir -p $(LOGS_DIR)
	@mkdir -p $(BUILD_DIR)
	@echo "✅ 项目初始化完成"

# 发布相关
.PHONY: release
release: clean test build-all ## 创建发布版本
	@echo "🚀 创建发布版本..."
	@echo "版本: $(VERSION)"
	@echo "构建时间: $(BUILD_TIME)"
	@echo "提交哈希: $(COMMIT_HASH)"
	@echo "✅ 发布版本创建完成"

# 运行相关
.PHONY: run
run: wire ## 运行应用程序
	@echo "🚀 运行应用程序..."
	$(GOCMD) run main.go

.PHONY: run-start
run-start: wire ## 运行 start 命令
	@echo "🚀 运行 start 命令..."
	$(GOCMD) run main.go start

.PHONY: run-endpoint
run-endpoint: wire ## 运行 endpoint 命令
	@echo "🚀 运行 endpoint 命令..."
	$(GOCMD) run main.go endpoint

# 监控和日志
.PHONY: logs
logs: ## 查看日志
	@echo "📋 查看日志..."
	@if [ -d $(LOGS_DIR) ]; then \
		tail -f $(LOGS_DIR)/*.log; \
	else \
		echo "⚠️  日志目录不存在: $(LOGS_DIR)"; \
	fi

# 健康检查
.PHONY: check
check: fmt vet lint test ## 运行所有检查
	@echo "✅ 所有检查完成"

# 快速开发
.PHONY: quick
quick: wire build ## 快速构建
	@echo "⚡ 快速构建完成"

# 显示项目信息
.PHONY: info
info: ## 显示项目信息
	@echo "项目信息:"
	@echo "  名称: $(PROJECT_NAME)"
	@echo "  模块: $(MODULE_NAME)"
	@echo "  版本: $(VERSION)"
	@echo "  构建时间: $(BUILD_TIME)"
	@echo "  提交哈希: $(COMMIT_HASH)"
	@echo "  Go 版本: $$($(GOCMD) version)"