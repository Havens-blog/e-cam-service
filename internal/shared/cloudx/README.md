# CloudX - 多云适配器架构

## 目录结构

```
internal/shared/cloudx/
├── types/                      # 共享类型定义
│   └── iam.go                  # IAM相关通用类型
│
├── common/                     # 通用组件
│   ├── retry/
│   │   └── backoff.go          # 指数退避重试逻辑
│   └── aliyun/
│       ├── client.go           # 阿里云客户端创建
│       ├── ratelimit.go        # 阿里云限流器
│       └── error.go            # 阿里云错误处理
│
├── iam/                        # IAM产品适配器
│   ├── adapter.go              # IAM适配器接口定义
│   ├── factory.go              # IAM适配器工厂
│   └── aliyun/                 # 阿里云IAM适配器实现
│       ├── adapter.go          # 核心适配器实现
│       ├── converter.go        # 数据转换工具
│       ├── types.go            # 阿里云特定类型
│       └── wrapper.go          # 接口包装器
│
└── (未来扩展)
    ├── compute/                # 计算资源适配器 (ECS/EC2/CVM)
    ├── storage/                # 存储适配器 (OSS/S3)
    └── database/               # 数据库适配器 (RDS)
```

## 设计原则

### 1. 按产品维度组织

- 每个云产品（IAM、计算、存储等）有独立的目录
- 每个产品定义自己的适配器接口和工厂
- 便于不同团队独立开发和维护

### 2. 按云厂商隔离实现

- 每个云厂商的实现在独立的子目录中
- 避免不同云厂商代码相互影响
- 新增云厂商只需添加新的子目录

### 3. 通用逻辑复用

- `types/` 包含跨云厂商的共享类型
- `common/` 包含可复用的通用组件
- 每个云厂商的 `common/` 子目录包含该厂商特定的通用逻辑

### 4. 避免循环依赖

- 使用 `types` 包存放共享类型，避免包之间的循环导入
- 使用 wrapper 模式实现接口，隔离内部实现和外部接口

## IAM 适配器架构

### 接口定义 (`iam/adapter.go`)

```go
type CloudIAMAdapter interface {
    ValidateCredentials(ctx, account) error
    ListUsers(ctx, account) ([]*CloudUser, error)
    GetUser(ctx, account, userID) (*CloudUser, error)
    CreateUser(ctx, account, req) (*CloudUser, error)
    UpdateUserPermissions(ctx, account, userID, policies) error
    DeleteUser(ctx, account, userID) error
    ListPolicies(ctx, account) ([]PermissionPolicy, error)
}
```

### 阿里云实现层次

1. **Adapter** (`aliyun/adapter.go`)

   - 核心业务逻辑实现
   - 调用阿里云 RAM SDK
   - 使用内部类型 `CreateUserParams`

2. **Converter** (`aliyun/converter.go`)

   - RAM SDK 类型 → 领域模型转换
   - 数据格式化和解析

3. **Wrapper** (`aliyun/wrapper.go`)
   - 实现 `CloudIAMAdapter` 接口
   - 类型转换：`types.CreateUserRequest` → `CreateUserParams`
   - 对外暴露统一接口

### 通用组件

#### 限流器 (`common/aliyun/ratelimit.go`)

```go
rateLimiter := aliyun.NewRateLimiter(20) // 20 QPS
err := rateLimiter.Wait(ctx)
```

#### 错误处理 (`common/aliyun/error.go`)

```go
if aliyun.IsThrottlingError(err) {
    // 处理限流错误
}
```

#### 重试逻辑 (`common/retry/backoff.go`)

```go
err := retry.WithBackoff(ctx, 3, operation, isRetryable)
```

## 使用示例

### 创建适配器

```go
factory := iam.NewCloudIAMAdapterFactory(logger)
adapter, err := factory.CreateAdapter(domain.CloudProviderAliyun)
```

### 调用适配器方法

```go
// 验证凭证
err := adapter.ValidateCredentials(ctx, account)

// 列出用户
users, err := adapter.ListUsers(ctx, account)

// 创建用户
req := &types.CreateUserRequest{
    Username:    "test-user",
    DisplayName: "Test User",
    Email:       "test@example.com",
}
user, err := adapter.CreateUser(ctx, account, req)
```

## 扩展指南

### 添加新的云厂商（如 AWS）

1. 创建目录 `iam/aws/`
2. 实现核心适配器 `aws/adapter.go`
3. 实现数据转换 `aws/converter.go`
4. 实现接口包装器 `aws/wrapper.go`
5. 在 `factory.go` 中添加创建逻辑

```go
case domain.CloudProviderAWS:
    adapter := aws.NewAdapter(f.logger)
    return aws.NewAdapterWrapper(adapter), nil
```

### 添加新的产品（如计算资源）

1. 创建目录 `compute/`
2. 定义接口 `compute/adapter.go`
3. 实现工厂 `compute/factory.go`
4. 为每个云厂商创建子目录：
   - `compute/aliyun/`
   - `compute/aws/`
   - `compute/huawei/`

## 最佳实践

### 1. 类型定义

- 共享类型放在 `types/` 包
- 云厂商特定类型放在各自的 `types.go`
- 使用清晰的命名避免混淆

### 2. 错误处理

- 使用 `fmt.Errorf` 包装错误，保留错误链
- 记录详细的日志信息
- 区分可重试和不可重试的错误

### 3. 限流和重试

- 所有 API 调用前检查限流
- 使用指数退避重试策略
- 设置合理的超时时间

### 4. 数据转换

- 集中在 `converter.go` 中处理
- 处理时区和时间格式
- 验证必填字段

### 5. 测试

- 为每个适配器编写单元测试
- 使用 Mock 隔离外部依赖
- 编写集成测试验证完整流程

## 注意事项

1. **避免循环导入**

   - 不要在子包中导入父包
   - 使用 `types` 包共享类型

2. **保持接口稳定**

   - 接口变更影响所有实现
   - 使用可选参数扩展功能

3. **日志规范**

   - 记录关键操作和错误
   - 包含足够的上下文信息
   - 避免记录敏感信息

4. **性能考虑**
   - 使用连接池复用客户端
   - 实现批量操作接口
   - 合理设置并发数
