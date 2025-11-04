# 适配器使用示例

## 核心优势：业务代码统一，无需关心云厂商差异

### 问题：每个云厂商 API 都不一样

```go
// ❌ 没有适配器的情况：需要为每个云厂商写不同的代码

func syncECS(account *CloudAccount) error {
    if account.Provider == "aliyun" {
        // 阿里云的调用方式
        client := aliyun.NewClient(account.AccessKeyID, account.AccessKeySecret)
        request := &aliyun.DescribeInstancesRequest{
            RegionId: "cn-hangzhou",
            PageSize: 100,
        }
        response, err := client.DescribeInstances(request)
        // 处理阿里云的返回格式...

    } else if account.Provider == "aws" {
        // AWS 的调用方式（完全不同）
        cfg := aws.NewConfig(account.AccessKeyID, account.AccessKeySecret)
        client := ec2.New(cfg)
        input := &ec2.DescribeInstancesInput{
            MaxResults: aws.Int32(100),
            Filters: []types.Filter{...},
        }
        response, err := client.DescribeInstances(input)
        // 处理 AWS 的返回格式（和阿里云完全不同）...

    } else if account.Provider == "azure" {
        // Azure 的调用方式（又是完全不同）
        // ...
    }

    // 每增加一个云厂商，这里就要加一堆 if-else
    // 代码重复、难以维护、容易出错
}
```

### 解决方案：使用适配器模式

```go
// ✅ 使用适配器后：业务代码统一，简洁清晰

func syncECS(account *CloudAccount) error {
    // 1. 创建适配器（工厂自动选择正确的实现）
    factory := adapter.NewAdapterFactory()
    cloudAdapter, err := factory.CreateAdapter(account)
    if err != nil {
        return err
    }

    // 2. 统一调用，不管是阿里云、AWS 还是 Azure
    instances, err := cloudAdapter.GetECSInstances(ctx, "cn-hangzhou")
    if err != nil {
        return err
    }

    // 3. 处理统一格式的数据
    for _, instance := range instances {
        fmt.Printf("实例: %s, 状态: %s, CPU: %d, 内存: %d MB\n",
            instance.InstanceName,
            instance.Status,
            instance.CPU,
            instance.Memory,
        )
    }

    // 完全不需要关心是哪个云厂商！
    // 新增云厂商只需要实现 CloudAdapter 接口，业务代码不用改
}
```

## 实际使用场景

### 场景 1：多云资源同步

```go
// internal/cam/service/asset.go

func (s *service) SyncCloudAccount(ctx context.Context, accountID int64) error {
    // 1. 获取云账号
    account, err := s.accountRepo.GetByID(ctx, accountID)
    if err != nil {
        return err
    }

    // 2. 创建适配器
    factory := adapter.NewAdapterFactory()
    cloudAdapter, err := factory.CreateAdapter(account)
    if err != nil {
        return err
    }

    // 3. 获取地域列表（统一接口）
    regions, err := cloudAdapter.GetRegions(ctx)
    if err != nil {
        return err
    }

    // 4. 遍历每个地域同步资源（统一接口）
    for _, region := range regions {
        instances, err := cloudAdapter.GetECSInstances(ctx, region.ID)
        if err != nil {
            log.Printf("同步地域 %s 失败: %v", region.ID, err)
            continue
        }

        // 5. 保存到数据库（统一格式）
        for _, instance := range instances {
            asset := convertToAsset(instance, account)
            s.assetRepo.Save(ctx, asset)
        }
    }

    return nil
}
```

### 场景 2：凭证验证

```go
func (s *service) TestCloudAccount(ctx context.Context, accountID int64) error {
    account, _ := s.accountRepo.GetByID(ctx, accountID)

    // 创建适配器
    factory := adapter.NewAdapterFactory()
    cloudAdapter, _ := factory.CreateAdapter(account)

    // 统一的验证接口，不管是哪个云厂商
    err := cloudAdapter.ValidateCredentials(ctx)
    if err != nil {
        return fmt.Errorf("凭证验证失败: %w", err)
    }

    return nil
}
```

### 场景 3：多云成本分析

```go
func (s *service) GetMultiCloudCost(ctx context.Context) (map[string]float64, error) {
    accounts, _ := s.accountRepo.ListAll(ctx)

    costs := make(map[string]float64)
    factory := adapter.NewAdapterFactory()

    for _, account := range accounts {
        // 为每个云账号创建适配器
        cloudAdapter, _ := factory.CreateAdapter(account)

        // 统一调用（不管是阿里云、AWS 还是 Azure）
        instances, _ := cloudAdapter.GetECSInstances(ctx, account.Region)

        // 统一计算成本
        totalCost := 0.0
        for _, instance := range instances {
            totalCost += instance.Cost
        }

        costs[string(account.Provider)] = totalCost
    }

    return costs, nil
}
```

## 适配器内部如何处理差异

### 阿里云适配器内部

```go
// internal/cam/sync/adapter/aliyun_adapter.go

func (a *AliyunAdapter) GetECSInstances(ctx context.Context, region string) ([]domain.ECSInstance, error) {
    // 1. 构造阿里云特有的请求
    request := &ecs.DescribeInstancesRequest{
        RegionId:   tea.String(region),      // 阿里云用 RegionId
        PageSize:   tea.Int32(100),          // 阿里云用 PageSize
        PageNumber: tea.Int32(1),
    }

    // 2. 调用阿里云 SDK
    response, _ := a.ecsClient.DescribeInstances(request)

    // 3. 转换为统一格式
    instances := make([]domain.ECSInstance, 0)
    for _, aliyunInstance := range response.Body.Instances.Instance {
        instance := domain.ECSInstance{
            InstanceID:   tea.StringValue(aliyunInstance.InstanceId),
            InstanceName: tea.StringValue(aliyunInstance.InstanceName),
            CPU:          int(tea.Int32Value(aliyunInstance.Cpu)),
            Memory:       int(tea.Int32Value(aliyunInstance.Memory)),
            // ... 其他字段映射
        }
        instances = append(instances, instance)
    }

    return instances, nil
}
```

### AWS 适配器内部

```go
// internal/cam/sync/adapter/aws_adapter.go

func (a *AWSAdapter) GetECSInstances(ctx context.Context, region string) ([]domain.ECSInstance, error) {
    // 1. 构造 AWS 特有的请求（和阿里云完全不同）
    input := &ec2.DescribeInstancesInput{
        MaxResults: aws.Int32(100),          // AWS 用 MaxResults
        Filters: []types.Filter{             // AWS 用 Filters
            {
                Name:   aws.String("instance-state-name"),
                Values: []string{"running", "stopped"},
            },
        },
    }

    // 2. 调用 AWS SDK
    response, _ := a.ec2Client.DescribeInstances(ctx, input)

    // 3. 转换为统一格式（和阿里云返回的格式完全不同，但我们转换成相同的）
    instances := make([]domain.ECSInstance, 0)
    for _, reservation := range response.Reservations {
        for _, awsInstance := range reservation.Instances {
            // AWS 的 CPU 和内存需要从 InstanceType 推断
            cpu, memory := parseAWSInstanceType(string(awsInstance.InstanceType))

            instance := domain.ECSInstance{
                InstanceID:   aws.ToString(awsInstance.InstanceId),
                InstanceName: getInstanceName(awsInstance.Tags), // AWS 从 Tags 中获取名称
                CPU:          cpu,
                Memory:       memory,
                // ... 其他字段映射
            }
            instances = append(instances, instance)
        }
    }

    return instances, nil
}
```

## 总结

### 适配器模式的价值

1. **业务代码统一**：不需要为每个云厂商写不同的代码
2. **易于扩展**：新增云厂商只需实现 CloudAdapter 接口
3. **易于测试**：可以 mock 适配器进行单元测试
4. **降低复杂度**：将云厂商差异封装在适配器内部

### 关键点

- **统一的接口**：`CloudAdapter` 定义统一的方法签名
- **统一的数据结构**：`ECSInstance` 定义统一的返回格式
- **适配器内部转换**：每个适配器负责将云厂商特有的 API 转换为统一格式
- **工厂模式**：`AdapterFactory` 根据云厂商类型创建对应的适配器

### 你只需要关心

1. 定义统一的接口和数据结构
2. 为每个云厂商实现适配器（一次性工作）
3. 业务代码使用统一接口（永远不需要改）

**适配器内部处理所有差异，业务代码完全不感知！**
