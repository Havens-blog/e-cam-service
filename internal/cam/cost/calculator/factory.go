package calculator

import (
	"fmt"
	
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	syncdomain "github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
)

// CostCalculator 成本计算器接口
// 不同云厂商的计费规则不同，需要不同的计算器
type CostCalculator interface {
	// CalculateInstanceCost 计算实例成本（每小时）
	CalculateInstanceCost(instance syncdomain.ECSInstance) float64
	
	// CalculateMonthlyCost 计算月度成本
	CalculateMonthlyCost(instance syncdomain.ECSInstance) float64
	
	// GetProvider 获取云厂商
	GetProvider() domain.CloudProvider
}

// CostCalculatorFactory 成本计算器工厂
type CostCalculatorFactory struct{}

// NewCostCalculatorFactory 创建成本计算器工厂
func NewCostCalculatorFactory() *CostCalculatorFactory {
	return &CostCalculatorFactory{}
}

// CreateCalculator 根据云厂商创建对应的成本计算器
func (f *CostCalculatorFactory) CreateCalculator(provider domain.CloudProvider) (CostCalculator, error) {
	switch provider {
	case domain.CloudProviderAliyun:
		return NewAliyunCostCalculator(), nil
		
	case domain.CloudProviderAWS:
		return NewAWSCostCalculator(), nil
		
	case domain.CloudProviderAzure:
		return NewAzureCostCalculator(), nil
		
	case domain.CloudProviderTencent:
		return NewTencentCostCalculator(), nil
		
	case domain.CloudProviderHuawei:
		return NewHuaweiCostCalculator(), nil
		
	default:
		return nil, fmt.Errorf("不支持的云厂商: %s", provider)
	}
}

// AliyunCostCalculator 阿里云成本计算器
type AliyunCostCalculator struct{}

func NewAliyunCostCalculator() *AliyunCostCalculator {
	return &AliyunCostCalculator{}
}

func (c *AliyunCostCalculator) GetProvider() domain.CloudProvider {
	return domain.CloudProviderAliyun
}

func (c *AliyunCostCalculator) CalculateInstanceCost(instance syncdomain.ECSInstance) float64 {
	// 阿里云计费规则
	// 这里是简化版本，实际应该查询阿里云价格 API
	
	baseCost := 0.0
	
	// 根据实例类型计算基础成本
	switch instance.InstanceTypeFamily {
	case "ecs.t5":
		baseCost = 0.05 // 每小时 0.05 元
	case "ecs.t6":
		baseCost = 0.08
	case "ecs.c6":
		baseCost = 0.15
	case "ecs.g6":
		baseCost = 0.20
	default:
		baseCost = 0.10
	}
	
	// 根据 CPU 和内存调整
	cpuFactor := float64(instance.CPU) * 0.02
	memoryFactor := float64(instance.Memory) / 1024 * 0.01
	
	// 公网带宽成本
	bandwidthCost := float64(instance.InternetMaxBandwidthOut) * 0.01
	
	// 磁盘成本
	diskCost := float64(instance.SystemDiskSize) * 0.001
	for _, disk := range instance.DataDisks {
		diskCost += float64(disk.Size) * 0.001
	}
	
	totalCost := baseCost + cpuFactor + memoryFactor + bandwidthCost + diskCost
	
	return totalCost
}

func (c *AliyunCostCalculator) CalculateMonthlyCost(instance syncdomain.ECSInstance) float64 {
	hourlyCost := c.CalculateInstanceCost(instance)
	// 按量付费：24小时 * 30天
	// 包年包月会有折扣，这里简化处理
	return hourlyCost * 24 * 30
}

// AWSCostCalculator AWS 成本计算器
type AWSCostCalculator struct{}

func NewAWSCostCalculator() *AWSCostCalculator {
	return &AWSCostCalculator{}
}

func (c *AWSCostCalculator) GetProvider() domain.CloudProvider {
	return domain.CloudProviderAWS
}

func (c *AWSCostCalculator) CalculateInstanceCost(instance syncdomain.ECSInstance) float64 {
	// AWS 计费规则（和阿里云不同）
	// 这里是简化版本，实际应该查询 AWS Pricing API
	
	baseCost := 0.0
	
	// AWS 的实例类型命名不同
	switch instance.InstanceTypeFamily {
	case "t2":
		baseCost = 0.0116 // 每小时 $0.0116
	case "t3":
		baseCost = 0.0104
	case "m5":
		baseCost = 0.096
	case "c5":
		baseCost = 0.085
	default:
		baseCost = 0.05
	}
	
	// AWS 按实例类型定价，不单独计算 CPU 和内存
	// 但我们可以根据配置调整
	sizeFactor := 1.0
	if instance.CPU >= 8 {
		sizeFactor = 2.0
	} else if instance.CPU >= 4 {
		sizeFactor = 1.5
	}
	
	// EBS 存储成本
	diskCost := float64(instance.SystemDiskSize) * 0.0001
	for _, disk := range instance.DataDisks {
		diskCost += float64(disk.Size) * 0.0001
	}
	
	totalCost := baseCost * sizeFactor + diskCost
	
	// 转换为人民币（假设汇率 7.0）
	return totalCost * 7.0
}

func (c *AWSCostCalculator) CalculateMonthlyCost(instance syncdomain.ECSInstance) float64 {
	hourlyCost := c.CalculateInstanceCost(instance)
	return hourlyCost * 24 * 30
}

// AzureCostCalculator Azure 成本计算器
type AzureCostCalculator struct{}

func NewAzureCostCalculator() *AzureCostCalculator {
	return &AzureCostCalculator{}
}

func (c *AzureCostCalculator) GetProvider() domain.CloudProvider {
	return domain.CloudProviderAzure
}

func (c *AzureCostCalculator) CalculateInstanceCost(instance syncdomain.ECSInstance) float64 {
	// Azure 计费规则（又不同）
	// TODO: 实现 Azure 计费逻辑
	return 0.10
}

func (c *AzureCostCalculator) CalculateMonthlyCost(instance syncdomain.ECSInstance) float64 {
	hourlyCost := c.CalculateInstanceCost(instance)
	return hourlyCost * 24 * 30
}

// TencentCostCalculator 腾讯云成本计算器
type TencentCostCalculator struct{}

func NewTencentCostCalculator() *TencentCostCalculator {
	return &TencentCostCalculator{}
}

func (c *TencentCostCalculator) GetProvider() domain.CloudProvider {
	return domain.CloudProviderTencent
}

func (c *TencentCostCalculator) CalculateInstanceCost(instance syncdomain.ECSInstance) float64 {
	// 腾讯云计费规则
	// TODO: 实现腾讯云计费逻辑
	return 0.08
}

func (c *TencentCostCalculator) CalculateMonthlyCost(instance syncdomain.ECSInstance) float64 {
	hourlyCost := c.CalculateInstanceCost(instance)
	return hourlyCost * 24 * 30
}

// HuaweiCostCalculator 华为云成本计算器
type HuaweiCostCalculator struct{}

func NewHuaweiCostCalculator() *HuaweiCostCalculator {
	return &HuaweiCostCalculator{}
}

func (c *HuaweiCostCalculator) GetProvider() domain.CloudProvider {
	return domain.CloudProviderHuawei
}

func (c *HuaweiCostCalculator) CalculateInstanceCost(instance syncdomain.ECSInstance) float64 {
	// 华为云计费规则
	// TODO: 实现华为云计费逻辑
	return 0.09
}

func (c *HuaweiCostCalculator) CalculateMonthlyCost(instance syncdomain.ECSInstance) float64 {
	hourlyCost := c.CalculateInstanceCost(instance)
	return hourlyCost * 24 * 30
}
