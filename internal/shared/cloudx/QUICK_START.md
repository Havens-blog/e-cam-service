# CloudX 快速开始指南

## 5 分钟快速上手

### 1. 使用现有适配器

```go
package main

import (
    "context"

    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam"
    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
    "github.com/gotomicro/ego/core/elog"
)

func main() {
    // 1. 创建工厂
    logger := elog.DefaultLogger
    factory := iam.NewCloudIAMAdapterFactory(logger)

    // 2. 获取适配器
    adapter, err := factory.CreateAdapter(domain.CloudProviderAliyun)
    if err != nil {
        panic(err)
    }

    // 3. 准备云账号信息
    account := &domain.CloudAccount{
        ID:              1,
        AccessKeyID:     "your-access-key",
        AccessKeySecret: "your-secret-key",
        TenantID:        "tenant-001",
    }

    ctx := context.Background()

    // 4. 验证凭证
    if err := adapter.ValidateCredentials(ctx, account); err != nil {
        panic(err)
    }

    // 5. 列出用户
    users, err := adapter.ListUsers(ctx, account)
    if err != nil {
        panic(err)
    }

    for _, user := range users {
        println("User:", user.Username)
    }

    // 6. 创建用户
    req := &types.CreateUserRequest{
        Username:    "new-user",
        DisplayName: "New User",
        Email:       "new@example.com",
    }

    newUser, err := adapter.CreateUser(ctx, account, req)
    if err != nil {
        panic(err)
    }

    println("Created user:", newUser.Username)
}
```

### 2. 添加新的云厂商（AWS 示例）

#### 步骤 1: 创建目录结构

```bash
mkdir -p internal/shared/cloudx/iam/aws
mkdir -p internal/shared/cloudx/common/aws
```

#### 步骤 2: 实现通用组件

**`common/aws/client.go`**

```go
package aws

import (
    "github.com/aws/aws-sdk-go-v2/service/iam"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

func CreateIAMClient(account *domain.CloudAccount) (*iam.Client, error) {
    // 创建 AWS IAM 客户端
    // ...
}
```

**`common/aws/ratelimit.go`**

```go
package aws

import (
    "context"
    "golang.org/x/time/rate"
)

type RateLimiter struct {
    limiter *rate.Limiter
}

func NewRateLimiter(qps int) *RateLimiter {
    return &RateLimiter{
        limiter: rate.NewLimiter(rate.Limit(qps), qps),
    }
}

func (r *RateLimiter) Wait(ctx context.Context) error {
    return r.limiter.Wait(ctx)
}
```

**`common/aws/error.go`**

```go
package aws

import "strings"

func IsThrottlingError(err error) bool {
    if err == nil {
        return false
    }
    errMsg := err.Error()
    return strings.Contains(errMsg, "Throttling") ||
        strings.Contains(errMsg, "TooManyRequests")
}
```

#### 步骤 3: 实现核心适配器

**`iam/aws/adapter.go`**

```go
package aws

import (
    "context"
    "fmt"

    awscommon "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/common/aws"
    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/common/retry"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
    "github.com/aws/aws-sdk-go-v2/service/iam"
    "github.com/gotomicro/ego/core/elog"
)

type Adapter struct {
    logger      *elog.Component
    rateLimiter *awscommon.RateLimiter
}

func NewAdapter(logger *elog.Component) *Adapter {
    return &Adapter{
        logger:      logger,
        rateLimiter: awscommon.NewRateLimiter(10), // 10 QPS
    }
}

func (a *Adapter) ValidateCredentials(ctx context.Context, account *domain.CloudAccount) error {
    if err := a.rateLimiter.Wait(ctx); err != nil {
        return err
    }

    client, err := awscommon.CreateIAMClient(account)
    if err != nil {
        return err
    }

    // 调用 AWS API 验证
    _, err = client.GetUser(ctx, &iam.GetUserInput{})
    return err
}

func (a *Adapter) ListUsers(ctx context.Context, account *domain.CloudAccount) ([]*domain.CloudUser, error) {
    if err := a.rateLimiter.Wait(ctx); err != nil {
        return nil, err
    }

    client, err := awscommon.CreateIAMClient(account)
    if err != nil {
        return nil, err
    }

    var allUsers []*domain.CloudUser

    // 分页获取用户
    paginator := iam.NewListUsersPaginator(client, &iam.ListUsersInput{})

    for paginator.HasMorePages() {
        page, err := paginator.NextPage(ctx)
        if err != nil {
            return nil, err
        }

        for _, iamUser := range page.Users {
            user := ConvertIAMUserToCloudUser(iamUser, account)
            allUsers = append(allUsers, user)
        }
    }

    return allUsers, nil
}

// 实现其他方法...
```

#### 步骤 4: 实现数据转换

**`iam/aws/converter.go`**

```go
package aws

import (
    "time"

    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
    "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

func ConvertIAMUserToCloudUser(iamUser types.User, account *domain.CloudAccount) *domain.CloudUser {
    now := time.Now()

    user := &domain.CloudUser{
        Username:       *iamUser.UserName,
        UserType:       domain.CloudUserTypeIAMUser,
        CloudAccountID: account.ID,
        Provider:       domain.CloudProviderAWS,
        CloudUserID:    *iamUser.UserId,
        Status:         domain.CloudUserStatusActive,
        TenantID:       account.TenantID,
        CreateTime:     *iamUser.CreateDate,
        UpdateTime:     now,
        CTime:          iamUser.CreateDate.Unix(),
        UTime:          now.Unix(),
        Metadata: domain.CloudUserMetadata{
            LastSyncTime: &now,
            Tags:         make(map[string]string),
        },
    }

    return user
}
```

#### 步骤 5: 实现接口包装器

**`iam/aws/wrapper.go`**

```go
package aws

import (
    "context"

    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

type CreateUserParams struct {
    Username string
    Path     string
    Tags     map[string]string
}

type AdapterWrapper struct {
    adapter *Adapter
}

func NewAdapterWrapper(adapter *Adapter) *AdapterWrapper {
    return &AdapterWrapper{adapter: adapter}
}

func (w *AdapterWrapper) ValidateCredentials(ctx context.Context, account *domain.CloudAccount) error {
    return w.adapter.ValidateCredentials(ctx, account)
}

func (w *AdapterWrapper) ListUsers(ctx context.Context, account *domain.CloudAccount) ([]*domain.CloudUser, error) {
    return w.adapter.ListUsers(ctx, account)
}

func (w *AdapterWrapper) CreateUser(ctx context.Context, account *domain.CloudAccount, req *types.CreateUserRequest) (*domain.CloudUser, error) {
    params := &CreateUserParams{
        Username: req.Username,
        Path:     "/",
    }
    return w.adapter.CreateUser(ctx, account, params)
}

// 实现其他接口方法...
```

#### 步骤 6: 更新工厂

**`iam/factory.go`**

```go
import (
    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/aliyun"
    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/aws"  // 新增
)

func (f *adapterFactory) CreateAdapter(provider domain.CloudProvider) (CloudIAMAdapter, error) {
    // ...

    switch provider {
    case domain.CloudProviderAliyun:
        adapter := aliyun.NewAdapter(f.logger)
        return aliyun.NewAdapterWrapper(adapter), nil

    case domain.CloudProviderAWS:  // 新增
        adapter := aws.NewAdapter(f.logger)
        return aws.NewAdapterWrapper(adapter), nil

    default:
        return nil, fmt.Errorf("不支持的云厂商: %s", provider)
    }
}
```

### 3. 添加新的产品（计算资源示例）

#### 步骤 1: 创建产品目录

```bash
mkdir -p internal/shared/cloudx/compute/{aliyun,aws}
```

#### 步骤 2: 定义接口

**`compute/adapter.go`**

```go
package compute

import (
    "context"

    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

type CloudComputeAdapter interface {
    // 列出实例
    ListInstances(ctx context.Context, account *domain.CloudAccount) ([]*domain.Instance, error)

    // 获取实例详情
    GetInstance(ctx context.Context, account *domain.CloudAccount, instanceID string) (*domain.Instance, error)

    // 创建实例
    CreateInstance(ctx context.Context, account *domain.CloudAccount, req *types.CreateInstanceRequest) (*domain.Instance, error)

    // 启动实例
    StartInstance(ctx context.Context, account *domain.CloudAccount, instanceID string) error

    // 停止实例
    StopInstance(ctx context.Context, account *domain.CloudAccount, instanceID string) error

    // 删除实例
    DeleteInstance(ctx context.Context, account *domain.CloudAccount, instanceID string) error
}
```

#### 步骤 3: 创建工厂

**`compute/factory.go`**

```go
package compute

import (
    "fmt"
    "sync"

    "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/compute/aliyun"
    "github.com/Havens-blog/e-cam-service/internal/shared/domain"
    "github.com/gotomicro/ego/core/elog"
)

type CloudComputeAdapterFactory interface {
    CreateAdapter(provider domain.CloudProvider) (CloudComputeAdapter, error)
}

type adapterFactory struct {
    adapters map[domain.CloudProvider]CloudComputeAdapter
    mu       sync.RWMutex
    logger   *elog.Component
}

func NewCloudComputeAdapterFactory(logger *elog.Component) CloudComputeAdapterFactory {
    return &adapterFactory{
        adapters: make(map[domain.CloudProvider]CloudComputeAdapter),
        logger:   logger,
    }
}

func (f *adapterFactory) CreateAdapter(provider domain.CloudProvider) (CloudComputeAdapter, error) {
    f.mu.RLock()
    if adapter, exists := f.adapters[provider]; exists {
        f.mu.RUnlock()
        return adapter, nil
    }
    f.mu.RUnlock()

    f.mu.Lock()
    defer f.mu.Unlock()

    var adapter CloudComputeAdapter
    var err error

    switch provider {
    case domain.CloudProviderAliyun:
        adapter, err = f.createAliyunAdapter()
    case domain.CloudProviderAWS:
        adapter, err = f.createAWSAdapter()
    default:
        return nil, fmt.Errorf("不支持的云厂商: %s", provider)
    }

    if err != nil {
        return nil, err
    }

    f.adapters[provider] = adapter
    return adapter, nil
}

func (f *adapterFactory) createAliyunAdapter() (CloudComputeAdapter, error) {
    adapter := aliyun.NewAdapter(f.logger)
    return aliyun.NewAdapterWrapper(adapter), nil
}

func (f *adapterFactory) createAWSAdapter() (CloudComputeAdapter, error) {
    return nil, fmt.Errorf("AWS 计算适配器尚未实现")
}
```

#### 步骤 4: 实现阿里云 ECS 适配器

按照 IAM 适配器的模式实现：

- `compute/aliyun/adapter.go` - 核心逻辑
- `compute/aliyun/converter.go` - 数据转换
- `compute/aliyun/wrapper.go` - 接口包装

## 常见问题

### Q1: 如何处理不同云厂商的特殊功能？

**A:** 在各云厂商的 adapter.go 中添加扩展方法，不需要修改接口。

```go
// aliyun/adapter.go
func (a *Adapter) EnableRAMConsoleLogin(ctx, account, userID, password string) error {
    // 阿里云特有功能
}
```

### Q2: 如何复用通用逻辑？

**A:** 将通用逻辑提取到 `common/` 目录。

```go
// 使用通用重试逻辑
err := retry.WithBackoff(ctx, 3, operation, isRetryable)

// 使用云厂商特定的限流器
rateLimiter := aliyun.NewRateLimiter(20)
```

### Q3: 如何处理类型转换？

**A:** 使用 wrapper 模式隔离接口和实现。

```go
// wrapper.go
func (w *AdapterWrapper) CreateUser(ctx, account, *types.CreateUserRequest) (*CloudUser, error) {
    // 转换为内部类型
    params := &CreateUserParams{...}
    return w.adapter.CreateUser(ctx, account, params)
}
```

### Q4: 如何测试适配器？

**A:** 为每个适配器编写单元测试和集成测试。

```go
// aliyun/adapter_test.go
func TestAdapter_ListUsers(t *testing.T) {
    // Mock RAM SDK
    // 测试逻辑
}
```

## 下一步

- 📖 阅读 [完整架构文档](README.md)
- 📖 查看 [重构总结](../../../REFACTORING_SUMMARY.md)
- 🔧 实现你的第一个适配器
- ✅ 编写测试用例

## 获取帮助

- 查看现有的阿里云 IAM 适配器实现作为参考
- 参考 `common/` 目录中的通用组件
- 遵循 Go 开发规范和项目编码标准
