# README 更新建议

## 建议在项目 README.md 中添加以下内容

### 多云 IAM 管理功能

#### 功能概述

e-cam-service 现已支持统一的多云 IAM 管理功能，可以通过统一的接口管理多个云厂商的用户、用户组和权限。

#### 支持的云厂商

| 云厂商     | 状态        | 功能                       |
| ---------- | ----------- | -------------------------- |
| 阿里云 RAM | ✅ 完全支持 | 用户、用户组、策略管理     |
| AWS IAM    | ✅ 完全支持 | 用户、用户组、策略管理     |
| 腾讯云 CAM | ✅ 完全支持 | 用户、用户组、策略管理     |
| 火山云     | ✅ 完全支持 | 用户、用户组、策略管理     |
| 华为云 IAM | ⏳ 基础支持 | 框架已就绪，API 调用待完善 |

#### 核心特性

- **统一接口**: 一套 API 管理所有云厂商
- **智能策略管理**: 自动对比并增量更新权限策略
- **限流保护**: 自动控制 API 调用频率，避免超限
- **错误重试**: 指数退避重试机制，提高可靠性
- **可扩展架构**: 易于添加新的云厂商支持

#### 快速开始

```go
import (
    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// 创建适配器工厂
factory := iam.New(logger)

// 获取阿里云适配器
adapter, err := factory.CreateAdapter(domain.CloudProviderAliyun)
if err != nil {
    log.Fatal(err)
}

// 获取用户列表
users, err := adapter.ListUsers(ctx, account)
if err != nil {
    log.Fatal(err)
}

// 获取用户组列表
groups, err := adapter.ListGroups(ctx, account)
if err != nil {
    log.Fatal(err)
}
```

#### 文档

- [需求文档](docs/multi-cloud-iam/requirements.md)
- [设计文档](docs/multi-cloud-iam/design.md)
- [实现文档](docs/IAM_GROUP_SYNC_IMPLEMENTATION.md)
- [项目完成报告](docs/PROJECT_COMPLETION_REPORT.md)

#### API 端点

```
# 用户管理
GET    /api/v1/cam/iam/users
POST   /api/v1/cam/iam/users
GET    /api/v1/cam/iam/users/:id
PUT    /api/v1/cam/iam/users/:id
DELETE /api/v1/cam/iam/users/:id

# 用户组管理
GET    /api/v1/cam/iam/groups
POST   /api/v1/cam/iam/groups
GET    /api/v1/cam/iam/groups/:id
PUT    /api/v1/cam/iam/groups/:id
DELETE /api/v1/cam/iam/groups/:id

# 同步任务
POST   /api/v1/cam/iam/sync/users
POST   /api/v1/cam/iam/sync/groups
GET    /api/v1/cam/iam/sync/tasks
```

#### 配置示例

```yaml
# config/prod.yaml
iam:
  rate_limits:
    aliyun: 20 # QPS
    aws: 10
    tencent: 15
    huawei: 15

  retry:
    max_attempts: 3
    backoff: exponential
```

#### 贡献

欢迎贡献代码，特别是：

- 华为云 IAM 适配器的完整实现
- 单元测试和集成测试
- 文档和示例

---

## 建议的 README 结构

```markdown
# e-cam-service

## 功能特性

- 多云资产管理
- **多云 IAM 管理** ⭐ NEW
- 云账号管理
- 资产同步
- ...

## 多云 IAM 管理

[添加上述内容]

## 快速开始

...

## API 文档

...

## 贡献指南

...
```
