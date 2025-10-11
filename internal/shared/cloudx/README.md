# CloudX - 多云厂商验证器

CloudX 是一个统一的多云厂商凭证验证组件，支持阿里云、AWS、Azure、腾讯云、华为云等主流云厂商的 AK/SK 验证。

## 特性

- **统一接口**: 所有云厂商使用相同的验证接口
- **格式验证**: 验证凭证格式是否符合各云厂商规范
- **真实验证**: 调用云厂商 API 进行真实的凭证验证
- **超时控制**: 支持 context 超时控制
- **错误处理**: 详细的错误分类和友好的错误信息
- **地域获取**: 获取云厂商支持的地域列表
- **权限检测**: 检测账号的基本权限范围

## 支持的云厂商

| 云厂商 | 状态 | 验证方式 |
|--------|------|----------|
| 阿里云 | ✅ 已实现 | ECS DescribeRegions API |
| AWS | 🚧 开发中 | STS GetCallerIdentity API |
| Azure | 🚧 开发中 | Resource Manager API |
| 腾讯云 | 🚧 开发中 | CVM DescribeRegions API |
| 华为云 | 🚧 开发中 | ECS ListServers API |

## 快速开始

### 1. 创建验证器

```go
import (
    "github.com/Havens-blog/e-cam-service/internal/cloudx"
    "github.com/Havens-blog/e-cam-service/internal/domain"
)

// 创建验证器工厂
factory := cloudx.NewCloudValidatorFactory()

// 创建阿里云验证器
validator, err := factory.CreateValidator(domain.CloudProviderAliyun)
if err != nil {
    log.Fatalf("创建验证器失败: %v", err)
}
```

### 2. 验证凭证

```go
// 准备账号信息
account := &domain.CloudAccount{
    Provider:        domain.CloudProviderAliyun,
    AccessKeyID:     "LTAI5tYourAccessKeyId123",
    AccessKeySecret: "YourAccessKeySecretHere123456",
    Region:          "cn-hangzhou",
}

// 创建 context
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

// 验证凭证
result, err := validator.ValidateCredentials(ctx, account)
if err != nil {
    log.Fatalf("验证失败: %v", err)
}

if result.Valid {
    fmt.Println("凭证验证成功!")
    fmt.Printf("支持的地域: %v\n", result.Regions)
    fmt.Printf("检测到的权限: %v\n", result.Permissions)
} else {
    fmt.Printf("凭证验证失败: %s\n", result.Message)
}
```

### 3. 获取地域列表

```go
regions, err := validator.GetSupportedRegions(ctx, account)
if err != nil {
    log.Printf("获取地域列表失败: %v", err)
} else {
    fmt.Printf("支持 %d 个地域: %v\n", len(regions), regions)
}
```

### 4. 测试连接

```go
err := validator.TestConnection(ctx, account)
if err != nil {
    fmt.Printf("连接测试失败: %v\n", err)
} else {
    fmt.Println("连接测试成功!")
}
```

## 阿里云凭证格式要求

- **AccessKeyId**: 24位字符，以 "LTAI" 开头
- **AccessKeySecret**: 30位字符
- **Region**: 有效的阿里云地域标识，如 "cn-hangzhou"

## 错误处理

验证器会返回以下类型的错误：

- `ErrInvalidCredentials`: 凭证无效（AccessKeyId 或 AccessKeySecret 错误）
- `ErrPermissionDenied`: 权限不足
- `ErrConnectionTimeout`: 连接超时
- `ErrUnsupportedProvider`: 不支持的云厂商
- `ErrRegionNotSupported`: 地域不支持

## 集成到服务中

在 CAM 服务中的使用示例：

```go
// 在 account service 中集成
func (s *cloudAccountService) TestConnection(ctx context.Context, id int64) (*domain.ConnectionTestResult, error) {
    // 获取账号信息
    account, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return nil, errs.AccountNotFound
    }

    // 创建验证器
    validator, err := s.validatorFactory.CreateValidator(account.Provider)
    if err != nil {
        return nil, fmt.Errorf("不支持的云厂商: %s", account.Provider)
    }

    // 执行验证
    result, err := validator.ValidateCredentials(ctx, account)
    if err != nil {
        return nil, err
    }

    // 返回测试结果
    return &domain.ConnectionTestResult{
        Status:   map[bool]string{true: "success", false: "failed"}[result.Valid],
        Message:  result.Message,
        Regions:  result.Regions,
        TestTime: result.ValidatedAt,
    }, nil
}
```

## 性能优化

1. **并发安全**: 验证器是无状态的，可以安全地并发使用
2. **超时控制**: 使用 context 控制 API 调用超时
3. **降级处理**: 当 API 调用失败时，返回默认地域列表
4. **错误缓存**: 可以考虑缓存验证结果，避免频繁的 API 调用

## 扩展新的云厂商

要添加新的云厂商支持，需要：

1. 实现 `CloudValidator` 接口
2. 在 `CloudValidatorFactory` 中添加对应的创建逻辑
3. 添加相应的错误处理
4. 编写单元测试

示例：

```go
type NewCloudValidator struct{}

func (v *NewCloudValidator) ValidateCredentials(ctx context.Context, account *domain.CloudAccount) (*ValidationResult, error) {
    // 实现验证逻辑
}

func (v *NewCloudValidator) GetSupportedRegions(ctx context.Context, account *domain.CloudAccount) ([]string, error) {
    // 实现地域获取逻辑
}

func (v *NewCloudValidator) TestConnection(ctx context.Context, account *domain.CloudAccount) error {
    // 实现连接测试逻辑
}
```

## 测试

运行测试：

```bash
# 运行所有测试
go test ./internal/cloudx -v

# 运行特定测试
go test ./internal/cloudx -run TestAliyunValidator -v

# 运行格式验证测试
go test ./internal/cloudx -run TestAliyunValidator_ValidateCredentialFormat -v
```

## 注意事项

1. **安全性**: 
   - 凭证信息会在内存中传递，确保在生产环境中妥善处理
   - 日志中不要输出完整的凭证信息
   - 使用 `MaskSensitiveData()` 方法脱敏显示

2. **网络环境**:
   - 确保服务器能够访问对应云厂商的 API 端点
   - 考虑网络代理和防火墙配置

3. **API 限制**:
   - 注意各云厂商的 API 调用频率限制
   - 考虑实现验证结果缓存机制

4. **错误处理**:
   - 区分网络错误、认证错误和权限错误
   - 提供友好的错误信息给用户