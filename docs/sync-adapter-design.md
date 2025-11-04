# 云资源同步适配器设计文档

## 概述

云资源同步适配器是一个抽象层，用于统一不同云厂商的 API 调用接口，实现多云资源的统一管理和同步。

## 架构设计

### 1. 核心接口

#### CloudAdapter 接口

```go
type CloudAdapter interface {
    GetProvider() CloudProvider
    ValidateCredentials(ctx context.Context) error
    GetECSInstances(ctx context.Context, region string) ([]ECSInstance, error)
    GetRegions(ctx context.Context) ([]Region, error)
}
```

### 2. 数据模型

#### CloudAccount - 云账号配置

存储云账号的认证信息和配置：

- **ID**: 账号唯一标识
- **Name**: 账号名称
- **Provider**: 云厂商类型（aliyun/aws/azure）
- **AccessKeyID**: 访问密钥 ID
- **AccessKeySecret**: 访问密钥 Secret（加密存储）
- **DefaultRegion**: 默认地域（如 cn-shenzhen）
- **Enabled**: 是否启用
- **Description**: 描述信息

#### SyncConfig - 同步配置

控制资源同步的行为：

- **AccountID**: 关联的账号 ID
- **ResourceTypes**: 要同步的资源类型列表
- **Regions**: 要同步的地域列表（为空表示所有地域）
- **SyncInterval**: 同步间隔（秒）
- **Enabled**: 是否启用自动同步
- **ConcurrentNum**: 并发数
- **LastSyncTime**: 上次同步时间
- **NextSyncTime**: 下次同步时间

#### ECSInstance - 云主机实例（通用格式）

统一的云主机数据格式，包含：

- **基本信息**: InstanceID, InstanceName, Status, Region, Zone
- **配置信息**: InstanceType, CPU, Memory, OSType, OSName
- **网络信息**: PublicIP, PrivateIP, VPCID
- **计费信息**: ChargeType, CreationTime, ExpiredTime
- **其他信息**: Tags, Description, Provider

## 实现细节

### 1. 阿里云适配器 (AliyunAdapter)

#### 配置项

```go
type AliyunConfig struct {
    AccessKeyID     string
    AccessKeySecret string
    DefaultRegion   string // 默认地域，如果为空则使用 cn-shenzhen
}
```

#### 关键特性

1. **客户端缓存**: 按地域缓存 ECS 客户端，避免重复创建
2. **默认地域**: 支持配置默认地域，用于获取地域列表等全局操作
3. **分页处理**: 自动处理大量实例的分页查询
4. **数据转换**: 将阿里云特有格式转换为通用格式
5. **错误处理**: 完善的错误包装和日志记录

#### 默认地域说明

- **用途**: 用于执行全局操作（如获取地域列表、验证凭证）
- **默认值**: `cn-shenzhen`（深圳）
- **可配置**: 可以从数据库中的账号配置读取
- **建议**: 选择网络延迟较低的地域作为默认地域

### 2. 适配器工厂 (AdapterFactory)

#### 创建方式

**方式 1: 从云账号配置创建**

```go
factory := adapters.NewAdapterFactory(logger)
adapter, err := factory.CreateAdapter(account)
```

适用场景：

- 从数据库读取账号配置后创建适配器
- 生产环境使用

**方式 2: 直接通过参数创建**

```go
adapter, err := factory.CreateAdapterByProvider(
    domain.ProviderAliyun,
    accessKeyID,
    accessKeySecret,
    defaultRegion,
)
```

适用场景：

- 测试和调试
- 临时创建适配器

## 使用示例

### 1. 从数据库配置创建适配器

```go
// 从数据库读取账号配置
account := &domain.CloudAccount{
    ID:              1,
    Name:            "生产环境阿里云账号",
    Provider:        domain.ProviderAliyun,
    AccessKeyID:     "LTAI...",
    AccessKeySecret: "encrypted_secret",
    DefaultRegion:   "cn-shenzhen",
    Enabled:         true,
}

// 创建适配器工厂
factory := adapters.NewAdapterFactory(logger)

// 创建适配器
adapter, err := factory.CreateAdapter(account)
if err != nil {
    return err
}

// 验证凭证
if err := adapter.ValidateCredentials(ctx); err != nil {
    return err
}

// 获取地域列表
regions, err := adapter.GetRegions(ctx)

// 获取指定地域的ECS实例
instances, err := adapter.GetECSInstances(ctx, "cn-beijing")
```

### 2. 多地域并发同步

```go
// 获取要同步的地域列表
regions, err := adapter.GetRegions(ctx)

// 使用 worker pool 并发同步
var wg sync.WaitGroup
semaphore := make(chan struct{}, 5) // 限制并发数为5

for _, region := range regions {
    wg.Add(1)
    go func(r domain.Region) {
        defer wg.Done()
        semaphore <- struct{}{}
        defer func() { <-semaphore }()

        instances, err := adapter.GetECSInstances(ctx, r.ID)
        if err != nil {
            logger.Error("同步失败", elog.String("region", r.ID), elog.FieldErr(err))
            return
        }

        // 保存实例数据
        saveInstances(instances)
    }(region)
}

wg.Wait()
```

## 扩展指南

### 添加新的云厂商适配器

1. **实现 CloudAdapter 接口**

```go
type AWSAdapter struct {
    accessKeyID     string
    accessKeySecret string
    defaultRegion   string
    logger          *elog.Component
}

func (a *AWSAdapter) GetProvider() domain.CloudProvider {
    return domain.ProviderAWS
}

func (a *AWSAdapter) ValidateCredentials(ctx context.Context) error {
    // 实现AWS凭证验证
}

func (a *AWSAdapter) GetECSInstances(ctx context.Context, region string) ([]domain.ECSInstance, error) {
    // 实现AWS EC2实例获取
    // 转换为通用格式
}

func (a *AWSAdapter) GetRegions(ctx context.Context) ([]domain.Region, error) {
    // 实现AWS地域列表获取
}
```

2. **在工厂中注册**

```go
func (f *AdapterFactory) CreateAdapter(account *domain.CloudAccount) (domain.CloudAdapter, error) {
    switch account.Provider {
    case domain.ProviderAliyun:
        return f.createAliyunAdapter(account), nil
    case domain.ProviderAWS:
        return f.createAWSAdapter(account), nil  // 新增
    // ...
    }
}
```

### 添加新的资源类型

1. **定义通用数据结构**

```go
type RDSInstance struct {
    InstanceID   string
    InstanceName string
    Engine       string
    EngineVersion string
    // ...
}
```

2. **扩展 CloudAdapter 接口**

```go
type CloudAdapter interface {
    // 现有方法
    GetECSInstances(ctx context.Context, region string) ([]ECSInstance, error)

    // 新增方法
    GetRDSInstances(ctx context.Context, region string) ([]RDSInstance, error)
}
```

3. **在各适配器中实现**

## 最佳实践

### 1. 错误处理

- 使用 `fmt.Errorf` 包装错误，保留错误链
- 记录详细的错误日志，包含地域、资源类型等上下文信息
- 区分可重试错误和不可重试错误

### 2. 性能优化

- 缓存客户端实例，避免重复创建
- 使用并发控制，避免过多并发请求
- 实现分页处理，避免一次性加载大量数据
- 考虑使用增量同步，只同步变更的资源

### 3. 安全性

- 敏感信息（AccessKeySecret）加密存储
- 使用环境变量或密钥管理服务存储凭证
- 定期轮换访问密钥
- 记录审计日志

### 4. 可观测性

- 记录详细的操作日志
- 添加 Prometheus 指标
- 实现健康检查接口
- 监控 API 调用频率和错误率

## 配置示例

### 数据库中的账号配置

```json
{
  "id": 1,
  "name": "生产环境阿里云账号",
  "provider": "aliyun",
  "access_key_id": "LTAI...",
  "access_key_secret": "encrypted_...",
  "default_region": "cn-shenzhen",
  "enabled": true,
  "description": "用于生产环境资源同步"
}
```

### 同步配置

```json
{
  "account_id": 1,
  "resource_types": ["ecs", "rds", "oss"],
  "regions": ["cn-beijing", "cn-shanghai", "cn-shenzhen"],
  "sync_interval": 3600,
  "enabled": true,
  "concurrent_num": 5
}
```

## 测试

### 单元测试

```bash
# 测试适配器基础功能
go run scripts/test_api.go

# 测试适配器工厂
go run scripts/test_adapter_factory.go
```

### 集成测试

```bash
# 设置环境变量
export ALIYUN_ACCESS_KEY_ID=your_key
export ALIYUN_ACCESS_KEY_SECRET=your_secret

# 运行集成测试
go run scripts/test_aliyun_adapter.go
```

## 未来规划

1. **支持更多云厂商**: AWS, Azure, 腾讯云, 华为云
2. **支持更多资源类型**: RDS, Redis, OSS, CDN, WAF 等
3. **实现增量同步**: 只同步变更的资源
4. **实现资源关联**: 建立资源之间的关联关系
5. **实现资源拓扑**: 可视化资源关系图
6. **实现成本分析**: 统计和分析云资源成本
