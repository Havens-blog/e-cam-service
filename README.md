# E-CAM Service

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

企业级云资产管理平台（Enterprise Cloud Asset Management），提供多云环境下的资产统一管理、自动发现、成本分析等核心功能。

## 📋 目录

- [核心功能](#核心功能)
- [项目架构](#项目架构)
- [技术栈](#技术栈)
- [快速开始](#快速开始)
- [部署指南](#部署指南)
- [使用文档](#使用文档)
- [开发指南](#开发指南)

## 🚀 核心功能

### 1. 多云资产管理 (CAM)

- **资产生命周期管理**：创建、更新、查询、删除云资产
- **批量资产操作**：支持批量创建和管理资产
- **资产自动发现**：自动发现云厂商资源并同步到平台
- **资产同步**：定期同步云资产状态和配置信息
- **统计分析**：资产分布统计、成本分析、趋势预测

### 2. 云账号管理

- **多云厂商支持**：阿里云、AWS、华为云、腾讯云、火山引擎（volcano/volcengine 同源别名）
- **Azure 边界（诚实声明）**：仅支持证书发现导入（Front Door / Application Gateway，经 Key Vault 读取）与凭证格式校验；无 CloudAdapter，资产同步/计费/日志/IAM/DNS 等能力运行时报 `ErrUnsupportedProvider`，属预期早失败
- **账号凭证管理**：安全存储和管理云账号 AK/SK
- **连接测试**：验证云账号凭证有效性
- **账号状态管理**：启用/禁用云账号
- **自动同步**：定期同步云账号下的资源

### 3. 异步任务框架

- **通用任务队列**：基于 channel 的高性能任务队列
- **Worker 池**：并发处理任务，提高执行效率
- **任务状态跟踪**：实时查询任务执行状态和进度
- **任务持久化**：MongoDB 存储任务历史记录
- **插件化执行器**：可扩展的任务执行器机制

### 4. 动态模型系统

- **自定义模型**：灵活定义资产模型结构
- **字段管理**：动态添加、修改、删除模型字段
- **字段分组**：组织和管理模型字段
- **模型关系**：定义模型之间的关联关系

### 5. IAM 身份与访问管理

- **用户管理**：云平台用户的统一管理和同步
- **用户组管理**：用户组的创建、更新、删除和权限配置
- **用户组成员同步**：🆕 自动同步云平台用户组及其成员关系
- **权限策略管理**：统一管理多云平台的权限策略
- **策略模板**：预定义权限策略模板，快速分配权限
- **审计日志**：记录所有 IAM 操作的审计日志
- **多租户支持**：基于租户的数据隔离和权限管理

### 6. 端点服务 (Endpoint)

- **服务端点管理**：管理各类服务端点配置
- **健康检查**：监控端点服务状态
- **负载均衡**：支持多端点负载分发

## 📁 项目架构

```
e-cam-service/
├── cmd/                          # 命令行工具
│   └── root.go                   # CLI 根命令
├── config/                       # 配置文件
│   ├── prod.yaml                 # 生产环境配置
│   └── logger.toml               # 日志配置
├── internal/                     # 内部包（不对外暴露）
│   ├── cam/                      # 🔥 云资产管理核心模块
│   │   ├── iam/                  # 🔥 IAM 身份与访问管理模块
│   │   │   ├── service/          # IAM 服务层
│   │   │   │   ├── user.go       # 用户服务
│   │   │   │   ├── group.go      # 用户组服务（含成员同步）
│   │   │   │   ├── template.go   # 策略模板服务
│   │   │   │   └── tenant.go     # 租户服务
│   │   │   ├── repository/       # IAM 仓储层
│   │   │   ├── web/              # IAM API 处理器
│   │   │   └── wire.go           # 依赖注入
│   │   ├── domain/               # 领域模型
│   │   │   ├── account.go        # 云账号模型
│   │   │   ├── asset.go          # 资产模型
│   │   │   ├── model.go          # 动态模型
│   │   │   └── field.go          # 字段定义
│   │   ├── repository/           # 数据访问层
│   │   │   ├── dao/              # 数据库操作
│   │   │   ├── account.go        # 账号仓储
│   │   │   └── asset.go          # 资产仓储
│   │   ├── service/              # 业务逻辑层
│   │   │   ├── account.go        # 账号服务
│   │   │   ├── asset.go          # 资产服务
│   │   │   └── model.go          # 模型服务
│   │   ├── sync/                 # 🔥 资产同步模块
│   │   │   ├── adapter/          # 云厂商适配器
│   │   │   ├── factory/          # 适配器工厂
│   │   │   └── service.go        # 同步服务
│   │   ├── task/                 # 🔥 异步任务模块
│   │   │   ├── executor/         # 任务执行器
│   │   │   ├── service/          # 任务服务
│   │   │   └── web/              # 任务API
│   │   ├── cost/                 # 成本分析模块
│   │   ├── web/                  # HTTP 处理器
│   │   │   ├── handler.go        # 主处理器
│   │   │   ├── vo.go             # 视图对象
│   │   │   └── swagger.go        # API 文档
│   │   ├── errs/                 # 错误定义
│   │   ├── module.go             # 模块初始化
│   │   └── wire.go               # 依赖注入
│   ├── endpoint/                 # 端点服务模块
│   │   ├── domain/               # 端点领域模型
│   │   ├── repository/           # 端点仓储
│   │   ├── service/              # 端点服务
│   │   └── web/                  # 端点API
│   └── shared/                   # 共享组件
│       ├── cloudx/               # 🔥 云厂商工具包
│       │   ├── iam/              # 🔥 IAM 云平台适配器
│       │   │   ├── adapter.go    # 适配器接口
│       │   │   ├── aliyun/       # 阿里云 RAM 适配器
│       │   │   ├── tencent/      # 腾讯云 CAM 适配器
│       │   │   └── factory.go    # 适配器工厂
│       │   ├── validator.go      # 凭证验证器
│       │   ├── aliyun_validator.go
│       │   ├── aws_validator.go
│       │   └── azure_validator.go
│       └── domain/               # 共享领域模型
├── pkg/                          # 🔥 可复用公共包
│   ├── taskx/                    # 通用异步任务框架
│   │   ├── task.go               # 任务接口定义
│   │   ├── queue.go              # 任务队列实现
│   │   ├── repository_mongo.go   # MongoDB 仓储
│   │   └── README.md             # 框架使用文档
│   ├── mongox/                   # MongoDB 封装
│   └── ginx/                     # Gin 框架扩展
├── ioc/                          # 依赖注入配置
│   ├── gin.go                    # HTTP 服务器配置
│   ├── wire.go                   # Wire 配置
│   └── wire_gen.go               # Wire 生成代码
├── api/                          # API 定义
├── docs/                         # 📚 文档目录
│   ├── async-task-framework.md   # 异步任务框架文档
│   ├── async-task-integration-summary.md
│   ├── ecs-sync-guide.md         # ECS 同步指南
│   └── API-DOCUMENTATION.md      # API 文档
├── scripts/                      # 工具脚本
│   ├── test_async_task.go        # 异步任务测试
│   ├── test_ecs_sync.go          # ECS 同步测试
│   ├── test_group_member_sync.go # 🆕 用户组成员同步测试
│   └── init_models.go            # 模型初始化
├── deploy/                       # 部署配置
├── main.go                       # 程序入口
├── Dockerfile                    # Docker 镜像构建
├── docker-compose.yml            # Docker Compose 配置
└── Makefile                      # 构建脚本
```

### 核心目录说明

| 目录                      | 说明               | 主要功能                                 |
| ------------------------- | ------------------ | ---------------------------------------- |
| `internal/cam/`           | 云资产管理核心模块 | 资产管理、云账号管理、资产同步、成本分析 |
| `internal/cam/sync/`      | 资产同步模块       | 多云资产自动发现和同步                   |
| `internal/cam/task/`      | 异步任务模块       | 异步任务执行和管理                       |
| `internal/shared/cloudx/` | 云厂商工具包       | 多云厂商凭证验证和 SDK 封装              |
| `pkg/taskx/`              | 通用异步任务框架   | 可复用的异步任务处理框架                 |
| `pkg/mongox/`             | MongoDB 封装       | 数据库连接和操作封装                     |
| `pkg/ginx/`               | Gin 框架扩展       | HTTP 请求处理增强                        |

## 🛠 技术栈

### 后端框架

- **Go 1.21+**：主要开发语言
- **Gin**：HTTP Web 框架
- **Wire**：依赖注入框架
- **Ego**：微服务框架组件

### 数据存储

- **MongoDB 7.0**：主数据库
- **Redis 7.2**：缓存和会话存储

### 云厂商 SDK

- **阿里云 SDK**：`github.com/aliyun/alibaba-cloud-sdk-go`
- **AWS SDK v2**：`github.com/aws/aws-sdk-go-v2`
- **Azure**：无 SDK 依赖（证书发现经 net/http 直调 AAD/ARM/Key Vault REST）

### 开发工具

- **golangci-lint**：代码质量检查
- **Docker**：容器化部署

## 🚀 快速开始

### 前置要求

- Go 1.21 或更高版本
- Docker 和 Docker Compose
- MongoDB 7.0+
- Redis 7.2+

### 1. 克隆项目

```bash
git clone https://github.com/Havens-blog/e-cam-service.git
cd e-cam-service
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 启动依赖服务

使用 Docker Compose 启动 MongoDB 和 Redis：

```bash
docker-compose up -d mongodb redis
```

查看服务状态：

```bash
docker-compose ps
```

### 4. 配置环境变量

复制配置文件模板：

```bash
cp .env.example .env
```

编辑 `.env` 文件，配置必要的环境变量：

```bash
# MongoDB 配置
MONGO_URI=mongodb://admin:password@localhost:27017
MONGO_DATABASE=e_cam_service

# Redis 配置
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=password

# 服务配置
SERVER_PORT=8001
```

### 5. 初始化数据库

运行初始化脚本：

```bash
go run scripts/init_models.go
```

### 6. 启动服务

```bash
go run main.go
```

或使用 Makefile：

```bash
make run
```

服务启动后，访问：

- API 服务：http://localhost:8001
- Swagger 文档：http://localhost:8001/swagger/index.html

## 📦 部署指南

### Docker 部署

#### 1. 构建镜像

```bash
docker build -t e-cam-service:latest .
```

或使用 PowerShell 构建脚本：

```powershell
.\build.ps1
```

#### 2. 运行容器

```bash
docker run -d \
  --name e-cam-service \
  -p 8001:8001 \
  -e MONGO_URI=mongodb://admin:password@mongodb:27017 \
  -e REDIS_ADDR=redis:6379 \
  --network e-cam-network \
  e-cam-service:latest
```

### Docker Compose 部署

完整部署所有服务（包括管理界面）：

```bash
# 启动所有服务
docker-compose --profile tools up -d

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f e-cam-service
```

服务访问地址：

- E-CAM Service：http://localhost:8001
- MongoDB Express：http://localhost:8082
- Redis Commander：http://localhost:8081

### 生产环境部署

#### 1. 编译二进制文件

```bash
# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o e-cam-service main.go

# Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o e-cam-service.exe main.go
```

#### 2. 配置生产环境

编辑 `config/prod.yaml`：

```yaml
e-cam-service:
  port: "8001"

logger:
  default:
    level: "info"
    format: "json"
    outputPaths:
      - "stdout"
      - "/var/log/e-cam-service/app.log"

# MongoDB 配置
mongo:
  uri: "mongodb://username:password@mongodb-host:27017"
  database: "e_cam_service"
  timeout: 10s

# Redis 配置
redis:
  addr: "redis-host:6379"
  password: "your-redis-password"
  db: 0
```

#### 3. 使用 systemd 管理服务

创建服务文件 `/etc/systemd/system/e-cam-service.service`：

```ini
[Unit]
Description=E-CAM Service
After=network.target

[Service]
Type=simple
User=ecam
WorkingDirectory=/opt/e-cam-service
ExecStart=/opt/e-cam-service/e-cam-service
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable e-cam-service
sudo systemctl start e-cam-service
sudo systemctl status e-cam-service
```

## 📖 使用文档

### API 接口

#### 1. 云账号管理

**创建云账号**

```bash
curl -X POST http://localhost:8001/api/v1/cam/cloud-accounts \
  -H "Content-Type: application/json" \
  -d '{
    "name": "阿里云生产账号",
    "provider": "aliyun",
    "access_key": "your-access-key",
    "secret_key": "your-secret-key",
    "region": "cn-hangzhou",
    "description": "生产环境阿里云账号"
  }'
```

**测试连接**

```bash
curl -X POST http://localhost:8001/api/v1/cam/cloud-accounts/{id}/test-connection
```

#### 2. 资产管理

**创建资产**

```bash
curl -X POST http://localhost:8001/api/v1/cam/assets \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-server-01",
    "type": "ecs",
    "provider": "aliyun",
    "region": "cn-hangzhou",
    "account_id": "account-id",
    "resource_id": "i-bp1234567890",
    "status": "running"
  }'
```

**查询资产列表**

```bash
curl -X GET "http://localhost:8001/api/v1/cam/assets?page=1&size=20&provider=aliyun"
```

#### 3. 资产同步

**同步云账号资产**

```bash
curl -X POST http://localhost:8001/api/v1/cam/cloud-accounts/{id}/sync \
  -H "Content-Type: application/json" \
  -d '{
    "resource_types": ["ecs", "rds", "oss"]
  }'
```

**异步任务同步**

```bash
# 提交同步任务
curl -X POST http://localhost:8001/api/v1/cam/tasks/sync-assets \
  -H "Content-Type: application/json" \
  -d '{
    "account_id": "account-id",
    "resource_types": ["ecs"]
  }'

# 查询任务状态
curl -X GET http://localhost:8001/api/v1/cam/tasks/{task_id}
```

#### 4. IAM 用户组管理

**同步用户组及成员** 🆕

```bash
# 同步云平台用户组及其所有成员
curl -X POST "http://localhost:8001/api/v1/cam/iam/groups/sync?cloud_account_id=123" \
  -H "X-Tenant-ID: tenant-001" \
  -H "Content-Type: application/json"

# 响应示例
{
  "code": 0,
  "message": "success",
  "data": {
    "total_groups": 5,
    "created_groups": 2,
    "updated_groups": 3,
    "failed_groups": 0,
    "total_members": 15,
    "synced_members": 14,
    "failed_members": 1
  }
}
```

**查询用户组列表**

```bash
curl -X GET "http://localhost:8001/api/v1/cam/iam/groups?page=1&size=20" \
  -H "X-Tenant-ID: tenant-001"
```

**查询用户列表**

```bash
curl -X GET "http://localhost:8001/api/v1/cam/iam/users?page=1&size=20" \
  -H "X-Tenant-ID: tenant-001"
```

#### 5. 统计分析

**获取资产统计**

```bash
curl -X GET http://localhost:8001/api/v1/cam/assets/statistics
```

**成本分析**

```bash
curl -X GET "http://localhost:8001/api/v1/cam/assets/cost-analysis?start_date=2025-01-01&end_date=2025-01-31"
```

### 完整 API 文档

启动服务后访问 Swagger 文档：http://localhost:8001/swagger/index.html

或查看文档目录：

- [API 文档](docs/API-DOCUMENTATION.md)
- [Swagger 文档更新说明](docs/SWAGGER_UPDATED.md) 🆕
- [用户权限查询 API](docs/USER_PERMISSIONS_API.md) 🆕
- [异步任务框架](docs/async-task-framework.md)
- [ECS 同步指南](docs/ecs-sync-guide.md)
- [用户组成员同步功能](docs/USER_GROUP_MEMBER_SYNC.md) 🆕
- [用户组成员查询 API](docs/GROUP_MEMBERS_API.md) 🆕
- [用户组同步示例](docs/examples/sync_user_groups_example.md) 🆕
- [用户组同步问题修复](docs/GROUP_SYNC_FIXES.md) 🆕
- [用户组成员查询修复](docs/GROUP_MEMBERS_QUERY_FIX.md) 🆕
- [用户组成员数据修复指南](docs/GROUP_MEMBERS_DATA_FIX.md) 🆕
- [用户个人权限同步功能](docs/USER_PERSONAL_POLICIES_SYNC.md) 🆕
- [用户个人权限实现总结](docs/USER_POLICIES_IMPLEMENTATION_SUMMARY.md) 🆕
- [用户个人权限同步完整实现](docs/USER_POLICIES_SYNC_COMPLETE.md) 🆕
- [用户数量统计修复](docs/USER_COUNT_FIX.md) 🆕
- [用户组成员数量统计修复](docs/GROUP_MEMBER_COUNT_FIX.md) 🆕
- [Tenant ID 问题排查](docs/TROUBLESHOOTING_TENANT_ID.md) 🆕
- [云账号 Tenant ID 更新修复](docs/CLOUD_ACCOUNT_TENANT_ID_FIX.md) 🆕

## 🔧 开发指南

### 开发环境设置

1. 安装开发工具：

```bash
# 安装 Wire（依赖注入代码生成）
go install github.com/google/wire/cmd/wire@latest

# 安装 golangci-lint（代码检查）
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

2. 生成依赖注入代码：

```bash
cd internal/cam && wire
cd ../endpoint && wire
cd ../../ioc && wire
```

3. 运行测试：

```bash
# 运行所有测试
go test ./...

# 运行特定模块测试
go test ./internal/cam/service/...

# 生成测试覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 代码规范

项目遵循 [Golang 开发规范](.kiro/steering/golang-development-rules.md)，主要包括：

- 使用 `gofmt` 格式化代码
- 遵循 Go 命名约定
- 编写单元测试，覆盖率 ≥ 80%
- 使用 Wire 进行依赖注入
- 错误处理使用 `fmt.Errorf` 包装
- 添加必要的代码注释

### 添加新功能

1. 在 `internal/cam/` 下创建新模块
2. 定义领域模型（domain）
3. 实现仓储层（repository）
4. 实现服务层（service）
5. 实现 Web 层（web）
6. 更新 Wire 配置
7. 编写单元测试
8. 更新 API 文档

### 使用异步任务框架

查看 [异步任务框架文档](pkg/taskx/README.md) 了解如何：

- 创建自定义任务执行器
- 提交和管理任务
- 监控任务执行状态

## 📝 更新日志

查看 [CHANGELOG.md](CHANGELOG.md) 了解版本更新历史。

## 🤝 贡献指南

欢迎贡献代码！请遵循以下步骤：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'feat: Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

### Commit 规范

使用语义化提交信息：

- `feat`: 新功能
- `fix`: 修复 bug
- `docs`: 文档更新
- `style`: 代码格式调整
- `refactor`: 代码重构
- `test`: 测试相关
- `chore`: 构建/工具链更新

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 📧 联系方式

- 作者：Haven
- Email：1175248773@qq.com
- 项目地址：https://github.com/Havens-blog/e-cam-service

## 🙏 致谢

感谢以下开源项目：

- [Gin](https://github.com/gin-gonic/gin) - HTTP Web 框架
- [Wire](https://github.com/google/wire) - 依赖注入
- [Ego](https://github.com/gotomicro/ego) - 微服务框架
- [MongoDB Go Driver](https://github.com/mongodb/mongo-go-driver)
- [Redis Go Client](https://github.com/redis/go-redis)
