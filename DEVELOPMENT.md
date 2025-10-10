# E-Cam Service 开发指南

这是 E-Cam Service 项目的开发指南，包含了开发环境设置、常用命令和最佳实践。

## 快速开始

### 1. 环境要求

**必需工具：**
- Go 1.24.1+
- Git
- Make (推荐)

**可选工具：**
- Docker & Docker Compose (用于数据库服务)
- golangci-lint (代码检查)
- Wire (依赖注入代码生成)

### 2. 快速设置

**Linux/macOS:**
```bash
# 运行设置脚本
./scripts/dev-setup.sh

# 或者手动设置
make init
```

**Windows:**
```cmd
# 运行设置脚本
scripts\dev-setup.bat

# 或者手动设置
make init
```

### 3. 启动开发服务器

```bash
make dev
```

## 项目结构

```
e-cam-service/
├── api/                    # API 定义文件
├── cmd/                    # 命令行工具
├── config/                 # 配置文件
├── deploy/                 # 部署相关文件
├── docs/                   # 文档
├── internal/               # 内部包
│   └── endpoint/           # 端点模块
├── ioc/                    # 依赖注入配置
├── pkg/                    # 公共包
├── scripts/                # 脚本文件
├── logs/                   # 日志文件
├── build/                  # 构建输出
├── dist/                   # 分发文件
├── Makefile               # 构建工具
├── docker-compose.yml     # 数据库服务
├── .env.example           # 环境变量模板
└── .golangci.yml          # 代码检查配置
```

## 常用命令

### 开发命令

```bash
# 查看所有可用命令
make help

# 启动开发服务器
make dev

# 构建应用程序
make build

# 运行测试
make test

# 格式化代码
make fmt

# 代码检查
make lint

# 生成 Wire 代码
make wire
```

### 数据库命令

```bash
# 启动数据库服务
make db-up

# 停止数据库服务
make db-down
```

### 构建和部署

```bash
# 构建当前平台
make build

# 构建所有平台
make build-all

# 创建发布版本
make release

# 清理构建文件
make clean
```

## 开发工作流

### 1. 功能开发

1. **创建功能分支**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **开发前准备**
   ```bash
   # 更新依赖
   make deps
   
   # 生成 Wire 代码
   make wire
   
   # 运行测试确保环境正常
   make test
   ```

3. **开发过程**
   ```bash
   # 启动开发服务器
   make dev
   
   # 在另一个终端运行测试
   make test
   
   # 格式化代码
   make fmt
   
   # 代码检查
   make lint
   ```

4. **提交前检查**
   ```bash
   # 运行所有检查
   make check
   ```

### 2. 测试

```bash
# 运行所有测试
make test

# 运行测试覆盖率
make test-coverage

# 运行竞态检测测试
make test-race

# 运行基准测试
make benchmark
```

### 3. 代码质量

```bash
# 格式化代码
make fmt

# 代码检查
make lint

# Go vet 检查
make vet

# 运行所有质量检查
make check
```

## 配置管理

### 环境变量

1. 复制环境变量模板：
   ```bash
   cp .env.example .env
   ```

2. 根据需要修改 `.env` 文件中的配置

### 数据库配置

项目使用 MongoDB 和 Redis，可以通过以下方式启动：

```bash
# 使用 Docker Compose 启动
make db-up

# 访问管理界面
# MongoDB: http://localhost:8082
# Redis: http://localhost:8081
```

## Wire 依赖注入

项目使用 Google Wire 进行依赖注入：

```bash
# 生成 Wire 代码
make wire

# 手动生成
wire gen ./ioc
wire gen ./internal/endpoint
```

### Wire 配置文件

- `ioc/wire.go` - 主要的依赖注入配置
- `internal/endpoint/wire.go` - 端点模块的依赖注入配置

## 脚本说明

### 开发脚本

- `scripts/dev-setup.sh` - 开发环境设置脚本
- `scripts/dev-setup.bat` - Windows 开发环境设置脚本

### 构建脚本

- `scripts/build.sh` - 构建脚本
- `scripts/test.sh` - 测试脚本
- `scripts/deploy.sh` - 部署脚本

## 最佳实践

### 1. 代码规范

- 使用 `gofmt` 格式化代码
- 使用 `golangci-lint` 进行代码检查
- 遵循 Go 官方代码规范
- 添加适当的注释和文档

### 2. 测试

- 为新功能编写单元测试
- 保持测试覆盖率在 80% 以上
- 使用表驱动测试
- 编写集成测试

### 3. 提交规范

- 使用清晰的提交信息
- 每个提交只包含一个逻辑变更
- 提交前运行 `make check`

### 4. 依赖管理

- 定期更新依赖：`make deps-update`
- 使用 `go mod tidy` 清理依赖
- 避免引入不必要的依赖

## 故障排除

### 常见问题

1. **Wire 生成失败**
   ```bash
   # 清理并重新生成
   make clean
   make wire
   ```

2. **测试失败**
   ```bash
   # 检查数据库服务是否启动
   make db-up
   
   # 重新运行测试
   make test
   ```

3. **构建失败**
   ```bash
   # 更新依赖
   make deps
   
   # 清理并重新构建
   make clean
   make build
   ```

### 获取帮助

- 查看 Makefile 中的所有命令：`make help`
- 查看项目文档：`docs/` 目录
- 检查日志文件：`logs/` 目录

## 贡献指南

1. Fork 项目
2. 创建功能分支
3. 提交变更
4. 推送到分支
5. 创建 Pull Request

确保在提交前运行：
```bash
make check
```

## 许可证

请查看 LICENSE 文件了解许可证信息。