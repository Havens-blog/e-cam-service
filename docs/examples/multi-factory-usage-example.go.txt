package example

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/adapter"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/converter"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/calculator"
	syncdomain "github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
)

// MultiCloudService 多云管理服务
// 展示如何组合使用多个工厂
type MultiCloudService struct {
	accountRepo       repository.CloudAccountRepository
	assetRepo         repository.CloudAssetRepository
	
	// 三个工厂
	adapterFactory    *adapter.AdapterFactory
	converterFactory  *converter.ConverterFactory
	calculatorFactory *calculator.CostCalculatorFactory
}

// NewMultiCloudService 创建多云管理服务
func NewMultiCloudService(
	accountRepo repository.CloudAccountRepository,
	assetRepo repository.CloudAssetRepository,
) *MultiCloudService {
	return &MultiCloudService{
		accountRepo:       accountRepo,
		assetRepo:         assetRepo,
		adapterFactory:    adapter.NewAdapterFactory(),
		converterFactory:  converter.NewConverterFactory(),
		calculatorFactory: calculator.NewCostCalculatorFactory(),
	}
}

// 场景 1：完整的资源同步流程（使用三个工厂）
func (s *MultiCloudService) SyncAccountWithCost(ctx context.Context, accountID int64) error {
	// 1. 获取云账号
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("获取账号失败: %w", err)
	}
	
	// 2. 使用适配器工厂创建云厂商适配器
	cloudAdapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return fmt.Errorf("创建适配器失败: %w", err)
	}
	
	// 3. 使用转换器工厂创建资源转换器
	resourceConverter, err := s.converterFactory.CreateConverter("cloud_ecs")
	if err != nil {
		return fmt.Errorf("创建转换器失败: %w", err)
	}
	
	// 4. 使用计算器工厂创建成本计算器
	costCalculator, err := s.calculatorFactory.CreateCalculator(account.Provider)
	if err != nil {
		return fmt.Errorf("创建成本计算器失败: %w", err)
	}
	
	// 5. 获取云资源（通过适配器）
	instances, err := cloudAdapter.GetECSInstances(ctx, account.Region)
	if err != nil {
		return fmt.Errorf("获取实例失败: %w", err)
	}
	
	// 6. 处理每个实例
	for _, instance := range instances {
		// 6.1 转换为统一的数据库模型（通过转换器）
		asset, err := resourceConverter.Convert(instance)
		if err != nil {
			fmt.Printf("转换实例失败: %v\n", err)
			continue
		}
		
		// 6.2 计算成本（通过成本计算器）
		monthlyCost := costCalculator.CalculateMonthlyCost(instance)
		asset.Cost = monthlyCost
		
		// 6.3 保存到数据库
		if err := s.assetRepo.Save(ctx, asset); err != nil {
			fmt.Printf("保存资产失败: %v\n", err)
			continue
		}
		
		fmt.Printf("✅ 同步实例: %s, 成本: %.2f 元/月\n", 
			instance.InstanceName, monthlyCost)
	}
	
	return nil
}

// 场景 2：多云成本对比（使用适配器工厂和计算器工厂）
func (s *MultiCloudService) CompareMultiCloudCost(ctx context.Context) (map[string]float64, error) {
	// 获取所有云账号
	accounts, _, err := s.accountRepo.List(ctx, domain.CloudAccountFilter{})
	if err != nil {
		return nil, err
	}
	
	costByProvider := make(map[string]float64)
	
	for _, account := range accounts {
		// 为每个云账号创建适配器
		cloudAdapter, _ := s.adapterFactory.CreateAdapter(account)
		
		// 为每个云厂商创建成本计算器
		costCalculator, _ := s.calculatorFactory.CreateCalculator(account.Provider)
		
		// 获取实例
		instances, _ := cloudAdapter.GetECSInstances(ctx, account.Region)
		
		// 计算总成本
		totalCost := 0.0
		for _, instance := range instances {
			cost := costCalculator.CalculateMonthlyCost(instance)
			totalCost += cost
		}
		
		costByProvider[string(account.Provider)] = totalCost
	}
	
	return costByProvider, nil
}

// 场景 3：批量同步多个账号（使用三个工厂）
func (s *MultiCloudService) BatchSyncAccounts(ctx context.Context, accountIDs []int64) error {
	for _, accountID := range accountIDs {
		account, _ := s.accountRepo.GetByID(ctx, accountID)
		
		// 每个账号都使用工厂创建对应的组件
		cloudAdapter, _ := s.adapterFactory.CreateAdapter(account)
		resourceConverter, _ := s.converterFactory.CreateConverter("cloud_ecs")
		costCalculator, _ := s.calculatorFactory.CreateCalculator(account.Provider)
		
		instances, _ := cloudAdapter.GetECSInstances(ctx, account.Region)
		
		for _, instance := range instances {
			asset, _ := resourceConverter.Convert(instance)
			asset.Cost = costCalculator.CalculateMonthlyCost(instance)
			s.assetRepo.Save(ctx, asset)
		}
		
		fmt.Printf("✅ 账号 %s 同步完成\n", account.Name)
	}
	
	return nil
}

// 场景 4：成本优化建议（使用适配器工厂和计算器工厂）
func (s *MultiCloudService) GetCostOptimizationSuggestions(ctx context.Context, accountID int64) ([]string, error) {
	account, _ := s.accountRepo.GetByID(ctx, accountID)
	
	// 创建适配器和计算器
	cloudAdapter, _ := s.adapterFactory.CreateAdapter(account)
	costCalculator, _ := s.calculatorFactory.CreateCalculator(account.Provider)
	
	instances, _ := cloudAdapter.GetECSInstances(ctx, account.Region)
	
	suggestions := make([]string, 0)
	
	for _, instance := range instances {
		cost := costCalculator.CalculateMonthlyCost(instance)
		
		// 分析成本，给出建议
		if cost > 1000 && instance.Status == "stopped" {
			suggestions = append(suggestions, 
				fmt.Sprintf("实例 %s 已停止但仍在计费，建议释放或转为按量付费", 
					instance.InstanceName))
		}
		
		if instance.CPU >= 8 && cost > 2000 {
			suggestions = append(suggestions, 
				fmt.Sprintf("实例 %s 配置较高，建议评估是否可以降配", 
					instance.InstanceName))
		}
		
		if len(instance.DataDisks) > 5 {
			suggestions = append(suggestions, 
				fmt.Sprintf("实例 %s 挂载了 %d 个数据盘，建议整合存储", 
					instance.InstanceName, len(instance.DataDisks)))
		}
	}
	
	return suggestions, nil
}

// 场景 5：资源迁移评估（使用多个计算器工厂）
func (s *MultiCloudService) EvaluateMigration(
	ctx context.Context,
	sourceAccountID int64,
	targetProvider domain.CloudProvider,
) (*MigrationReport, error) {
	// 获取源账号
	sourceAccount, _ := s.accountRepo.GetByID(ctx, sourceAccountID)
	
	// 创建源云厂商的适配器和计算器
	sourceAdapter, _ := s.adapterFactory.CreateAdapter(sourceAccount)
	sourceCalculator, _ := s.calculatorFactory.CreateCalculator(sourceAccount.Provider)
	
	// 创建目标云厂商的计算器
	targetCalculator, _ := s.calculatorFactory.CreateCalculator(targetProvider)
	
	// 获取源云厂商的实例
	instances, _ := sourceAdapter.GetECSInstances(ctx, sourceAccount.Region)
	
	report := &MigrationReport{
		SourceProvider: sourceAccount.Provider,
		TargetProvider: targetProvider,
		InstanceCount:  len(instances),
	}
	
	// 计算迁移前后的成本对比
	for _, instance := range instances {
		sourceCost := sourceCalculator.CalculateMonthlyCost(instance)
		targetCost := targetCalculator.CalculateMonthlyCost(instance)
		
		report.SourceTotalCost += sourceCost
		report.TargetTotalCost += targetCost
	}
	
	report.CostSaving = report.SourceTotalCost - report.TargetTotalCost
	report.SavingPercentage = (report.CostSaving / report.SourceTotalCost) * 100
	
	return report, nil
}

// MigrationReport 迁移评估报告
type MigrationReport struct {
	SourceProvider    domain.CloudProvider
	TargetProvider    domain.CloudProvider
	InstanceCount     int
	SourceTotalCost   float64
	TargetTotalCost   float64
	CostSaving        float64
	SavingPercentage  float64
}

// 场景 6：定时任务 - 每日成本统计（使用所有工厂）
func (s *MultiCloudService) DailyCostStatistics(ctx context.Context) error {
	accounts, _, _ := s.accountRepo.List(ctx, domain.CloudAccountFilter{})
	
	for _, account := range accounts {
		// 为每个账号创建所需的组件
		cloudAdapter, _ := s.adapterFactory.CreateAdapter(account)
		costCalculator, _ := s.calculatorFactory.CreateCalculator(account.Provider)
		
		instances, _ := cloudAdapter.GetECSInstances(ctx, account.Region)
		
		dailyCost := 0.0
		for _, instance := range instances {
			hourlyCost := costCalculator.CalculateInstanceCost(instance)
			dailyCost += hourlyCost * 24
		}
		
		// 保存每日成本记录
		fmt.Printf("📊 %s - %s: %.2f 元/天\n", 
			time.Now().Format("2006-01-02"),
			account.Name,
			dailyCost)
	}
	
	return nil
}

// 总结：工厂模式的价值
// 
// 1. 适配器工厂：根据云厂商创建对应的 API 适配器
//    - 阿里云 → AliyunAdapter
//    - AWS → AWSAdapter
//    - Azure → AzureAdapter
//
// 2. 转换器工厂：根据资源类型创建对应的转换器
//    - cloud_ecs → ECSConverter
//    - cloud_rds → RDSConverter
//    - cloud_oss → OSSConverter
//
// 3. 计算器工厂：根据云厂商创建对应的成本计算器
//    - 阿里云 → AliyunCostCalculator
//    - AWS → AWSCostCalculator
//    - Azure → AzureCostCalculator
//
// 业务代码只需要：
// 1. 调用工厂创建对象
// 2. 使用统一的接口
// 3. 完全不关心具体实现
//
// 新增云厂商或资源类型时：
// 1. 实现对应的接口
// 2. 在工厂中添加一个 case
// 3. 所有业务代码不需要改动
