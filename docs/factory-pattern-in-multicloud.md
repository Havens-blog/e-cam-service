# 工厂模式在多云系统中的应用

## 1. 适配器工厂（你已经知道的）

```go
// 创建云厂商适配器
factory := adapter.NewAdapterFactory()
adapter, _ := factory.CreateAdapter(account)
```

## 2. 资源转换器工厂

不同云厂商的资源需要转换为统一的数据库模型

```go
// internal/cam/sync/converter/factory.go

type ConverterFactory struct{}

// 根据资源类型创建对应的转换器
func (f *ConverterFactory) CreateConverter(assetType string) (ResourceConverter, error) {
    switch assetType {
    case "cloud_ecs":
        return NewECSConverter(), nil
    case "cloud_rds":
        return NewRDSConverter(), nil
    case "cloud_oss":
        return NewOSSConverter(), nil
    case "cloud_cdn":
        return NewCDNConverter(), nil
    case "cloud_waf":
        return NewWAFConverter(), nil
    default:
        return nil, fmt.Errorf("不支持的资源类型: %s", assetType)
    }
}

// 使用示例
func syncResources(instances []ECSInstance) {
    factory := converter.NewConverterFactory()
    converter, _ := factory.CreateConverter("cloud_ecs")

    for _, instance := range instances {
        // 转换为数据库模型
        asset := converter.Convert(instance)
        repo.Save(asset)
    }
}
```

## 3. 成本计算器工厂

不同云厂商的计费规则不同

```go
// internal/cam/cost/calculator/factory.go

type CostCalculatorFactory struct{}

// 根据云厂商创建对应的成本计算器
func (f *CostCalculatorFactory) CreateCalculator(provider CloudProvider) (CostCalculator, error) {
    switch provider {
    case CloudProviderAliyun:
        return NewAliyunCostCalculator(), nil
    case CloudProviderAWS:
        return NewAWSCostCalculator(), nil
    case CloudProviderAzure:
        return NewAzureCostCalculator(), nil
    default:
        return nil, fmt.Errorf("不支持的云厂商: %s", provider)
    }
}

// 使用示例
func calculateMonthlyCost(account *CloudAccount, instances []ECSInstance) float64 {
    factory := calculator.NewCostCalculatorFactory()
    calc, _ := factory.CreateCalculator(account.Provider)

    totalCost := 0.0
    for _, instance := range instances {
        // 不同云厂商有不同的计费逻辑
        cost := calc.CalculateInstanceCost(instance)
        totalCost += cost
    }

    return totalCost
}
```

## 4. 监控指标采集器工厂

不同云厂商的监控 API 不同

```go
// internal/cam/monitor/collector/factory.go

type MetricsCollectorFactory struct{}

// 根据云厂商创建对应的监控采集器
func (f *MetricsCollectorFactory) CreateCollector(provider CloudProvider) (MetricsCollector, error) {
    switch provider {
    case CloudProviderAliyun:
        return NewAliyunMetricsCollector(), nil
    case CloudProviderAWS:
        return NewCloudWatchCollector(), nil
    case CloudProviderAzure:
        return NewAzureMonitorCollector(), nil
    default:
        return nil, fmt.Errorf("不支持的云厂商: %s", provider)
    }
}

// 使用示例
func collectMetrics(account *CloudAccount, instanceID string) (*Metrics, error) {
    factory := collector.NewMetricsCollectorFactory()
    collector, _ := factory.CreateCollector(account.Provider)

    // 采集 CPU、内存、网络等指标
    metrics, _ := collector.CollectInstanceMetrics(instanceID)
    return metrics, nil
}
```

## 5. 告警规则引擎工厂

不同资源类型需要不同的告警规则

```go
// internal/cam/alert/engine/factory.go

type AlertEngineFactory struct{}

// 根据资源类型创建对应的告警引擎
func (f *AlertEngineFactory) CreateEngine(assetType string) (AlertEngine, error) {
    switch assetType {
    case "cloud_ecs":
        return NewECSAlertEngine(), nil
    case "cloud_rds":
        return NewRDSAlertEngine(), nil
    case "cloud_cdn":
        return NewCDNAlertEngine(), nil
    default:
        return NewDefaultAlertEngine(), nil
    }
}

// 使用示例
func checkAlerts(asset *CloudAsset, metrics *Metrics) []Alert {
    factory := engine.NewAlertEngineFactory()
    engine, _ := factory.CreateEngine(asset.AssetType)

    // 不同资源类型有不同的告警规则
    // ECS: CPU > 80%, 内存 > 90%
    // RDS: 连接数 > 1000, 慢查询 > 100
    alerts := engine.CheckRules(asset, metrics)
    return alerts
}
```

## 6. 数据导出器工厂

支持导出为不同格式

```go
// internal/cam/export/factory.go

type ExporterFactory struct{}

// 根据导出格式创建对应的导出器
func (f *ExporterFactory) CreateExporter(format string) (Exporter, error) {
    switch format {
    case "excel":
        return NewExcelExporter(), nil
    case "csv":
        return NewCSVExporter(), nil
    case "json":
        return NewJSONExporter(), nil
    case "pdf":
        return NewPDFExporter(), nil
    default:
        return nil, fmt.Errorf("不支持的格式: %s", format)
    }
}

// 使用示例
func exportAssets(assets []CloudAsset, format string) ([]byte, error) {
    factory := export.NewExporterFactory()
    exporter, _ := factory.CreateExporter(format)

    // 导出为指定格式
    data, _ := exporter.Export(assets)
    return data, nil
}
```

## 7. 同步策略工厂

不同场景使用不同的同步策略

```go
// internal/cam/sync/strategy/factory.go

type SyncStrategyFactory struct{}

// 根据同步类型创建对应的策略
func (f *SyncStrategyFactory) CreateStrategy(syncType string) (SyncStrategy, error) {
    switch syncType {
    case "full":
        return NewFullSyncStrategy(), nil      // 全量同步
    case "incremental":
        return NewIncrementalSyncStrategy(), nil  // 增量同步
    case "realtime":
        return NewRealtimeSyncStrategy(), nil  // 实时同步
    default:
        return nil, fmt.Errorf("不支持的同步类型: %s", syncType)
    }
}

// 使用示例
func syncAccount(account *CloudAccount, syncType string) error {
    factory := strategy.NewSyncStrategyFactory()
    strategy, _ := factory.CreateStrategy(syncType)

    // 执行同步
    return strategy.Sync(account)
}
```

## 8. 资源操作器工厂

对不同云厂商的资源进行操作（启动、停止、删除等）

```go
// internal/cam/operation/factory.go

type ResourceOperatorFactory struct{}

// 根据云厂商创建对应的资源操作器
func (f *ResourceOperatorFactory) CreateOperator(provider CloudProvider) (ResourceOperator, error) {
    switch provider {
    case CloudProviderAliyun:
        return NewAliyunOperator(), nil
    case CloudProviderAWS:
        return NewAWSOperator(), nil
    case CloudProviderAzure:
        return NewAzureOperator(), nil
    default:
        return nil, fmt.Errorf("不支持的云厂商: %s", provider)
    }
}

// 使用示例
func startInstance(account *CloudAccount, instanceID string) error {
    factory := operation.NewResourceOperatorFactory()
    operator, _ := factory.CreateOperator(account.Provider)

    // 启动实例（不同云厂商的 API 不同）
    return operator.StartInstance(instanceID)
}

func stopInstance(account *CloudAccount, instanceID string) error {
    factory := operation.NewResourceOperatorFactory()
    operator, _ := factory.CreateOperator(account.Provider)

    // 停止实例
    return operator.StopInstance(instanceID)
}
```

## 9. 凭证验证器工厂（你项目中已有）

```go
// internal/cam/cloudx/validator.go

type CloudValidatorFactory struct{}

func (f *CloudValidatorFactory) CreateValidator(provider CloudProvider) (CloudValidator, error) {
    switch provider {
    case CloudProviderAliyun:
        return NewAliyunValidator(), nil
    case CloudProviderAWS:
        return NewAWSValidator(), nil
    case CloudProviderAzure:
        return NewAzureValidator(), nil
    default:
        return nil, ErrUnsupportedProvider
    }
}
```

## 10. 报表生成器工厂

生成不同类型的报表

```go
// internal/cam/report/factory.go

type ReportGeneratorFactory struct{}

// 根据报表类型创建对应的生成器
func (f *ReportGeneratorFactory) CreateGenerator(reportType string) (ReportGenerator, error) {
    switch reportType {
    case "cost_analysis":
        return NewCostAnalysisReportGenerator(), nil
    case "resource_inventory":
        return NewResourceInventoryReportGenerator(), nil
    case "compliance":
        return NewComplianceReportGenerator(), nil
    case "performance":
        return NewPerformanceReportGenerator(), nil
    default:
        return nil, fmt.Errorf("不支持的报表类型: %s", reportType)
    }
}

// 使用示例
func generateReport(reportType string, startDate, endDate time.Time) (*Report, error) {
    factory := report.NewReportGeneratorFactory()
    generator, _ := factory.CreateGenerator(reportType)

    // 生成报表
    report, _ := generator.Generate(startDate, endDate)
    return report, nil
}
```

## 实际项目中的组合使用

```go
// internal/cam/service/multi_cloud_service.go

type MultiCloudService struct {
    adapterFactory    *adapter.AdapterFactory
    converterFactory  *converter.ConverterFactory
    calculatorFactory *calculator.CostCalculatorFactory
    collectorFactory  *collector.MetricsCollectorFactory
    operatorFactory   *operation.ResourceOperatorFactory
}

// 完整的同步流程
func (s *MultiCloudService) SyncAndAnalyze(accountID int64) error {
    // 1. 获取账号
    account, _ := s.repo.GetByID(accountID)

    // 2. 创建适配器（获取资源）
    adapter, _ := s.adapterFactory.CreateAdapter(account)
    instances, _ := adapter.GetECSInstances(ctx, account.Region)

    // 3. 创建转换器（转换数据）
    converter, _ := s.converterFactory.CreateConverter("cloud_ecs")

    // 4. 创建成本计算器（计算成本）
    calculator, _ := s.calculatorFactory.CreateCalculator(account.Provider)

    // 5. 创建监控采集器（采集指标）
    collector, _ := s.collectorFactory.CreateCollector(account.Provider)

    for _, instance := range instances {
        // 转换并保存
        asset := converter.Convert(instance)

        // 计算成本
        asset.Cost = calculator.CalculateInstanceCost(instance)

        // 采集监控指标
        metrics, _ := collector.CollectInstanceMetrics(instance.InstanceID)
        asset.Metrics = metrics

        // 保存到数据库
        s.repo.Save(asset)
    }

    return nil
}

// 资源操作
func (s *MultiCloudService) BatchStartInstances(accountID int64, instanceIDs []string) error {
    account, _ := s.repo.GetByID(accountID)

    // 创建操作器
    operator, _ := s.operatorFactory.CreateOperator(account.Provider)

    for _, instanceID := range instanceIDs {
        operator.StartInstance(instanceID)
    }

    return nil
}
```

## 工厂模式的价值总结

### 在多云系统中的应用场景

1. **适配器工厂** - 创建云厂商适配器
2. **转换器工厂** - 创建资源转换器
3. **计算器工厂** - 创建成本计算器
4. **采集器工厂** - 创建监控采集器
5. **引擎工厂** - 创建告警引擎
6. **导出器工厂** - 创建数据导出器
7. **策略工厂** - 创建同步策略
8. **操作器工厂** - 创建资源操作器
9. **验证器工厂** - 创建凭证验证器
10. **生成器工厂** - 创建报表生成器

### 核心优势

1. **统一创建逻辑** - 所有对象创建都通过工厂
2. **易于扩展** - 新增类型只需要改工厂
3. **降低耦合** - 业务代码不依赖具体实现
4. **代码复用** - 避免重复的创建逻辑
5. **集中管理** - 所有创建规则在一个地方

### 项目结构示例

```
internal/cam/
├── sync/
│   ├── adapter/
│   │   ├── factory.go           # 适配器工厂
│   │   ├── aliyun_adapter.go
│   │   └── aws_adapter.go
│   ├── converter/
│   │   ├── factory.go           # 转换器工厂
│   │   ├── ecs_converter.go
│   │   └── rds_converter.go
│   └── strategy/
│       ├── factory.go           # 策略工厂
│       ├── full_sync.go
│       └── incremental_sync.go
├── cost/
│   └── calculator/
│       ├── factory.go           # 计算器工厂
│       ├── aliyun_calculator.go
│       └── aws_calculator.go
├── monitor/
│   └── collector/
│       ├── factory.go           # 采集器工厂
│       ├── aliyun_collector.go
│       └── cloudwatch_collector.go
└── operation/
    ├── factory.go               # 操作器工厂
    ├── aliyun_operator.go
    └── aws_operator.go
```

## 总结

工厂模式不只是"一小段代码"，它是多云系统中的**核心设计模式**：

- **适配器** 解决"API 差异"问题
- **工厂** 解决"对象创建"问题

在多云系统中，几乎每个需要根据条件创建不同对象的地方，都可以使用工厂模式。

**工厂模式让你的代码更灵活、更易扩展、更易维护。**
