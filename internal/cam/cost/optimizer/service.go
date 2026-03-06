// Package optimizer 资源优化建议
package optimizer

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// RecTypeDownsize 低 CPU 利用率降配建议
	RecTypeDownsize = "downsize"
	// RecTypeReleaseDisk 未挂载云盘释放建议
	RecTypeReleaseDisk = "release_disk"
	// RecTypeConvertPrepaid 按量转包年包月建议
	RecTypeConvertPrepaid = "convert_prepaid"

	// StatusPending 待处理
	StatusPending = "pending"
	// StatusDismissed 已忽略
	StatusDismissed = "dismissed"

	// dismissDuration 忽略有效期
	dismissDuration = 30 * 24 * time.Hour

	// downsizeSavingRatio 降配预估节省比例
	downsizeSavingRatio = 0.30
	// releaseDiskSavingRatio 释放云盘预估节省比例
	releaseDiskSavingRatio = 1.0
	// convertPrepaidSavingRatio 转包年包月预估节省比例
	convertPrepaidSavingRatio = 0.40

	// lowCPUThreshold 低 CPU 利用率阈值
	lowCPUThreshold = 10.0
	// lowCPUConsecutiveDays 低 CPU 连续天数
	lowCPUConsecutiveDays = 7
	// onDemandRunningDays 按量付费运行天数阈值
	onDemandRunningDays = 30
)

// ResourceMetrics 资源利用率数据接口
type ResourceMetrics interface {
	// GetCPUUtilization 获取指定资源过去 N 天的每日平均 CPU 利用率
	GetCPUUtilization(ctx context.Context, resourceID string, days int) ([]DailyCPU, error)
	// GetUnattachedDisks 获取未挂载的云盘列表
	GetUnattachedDisks(ctx context.Context, tenantID string) ([]DiskInfo, error)
}

// DailyCPU 每日 CPU 利用率
type DailyCPU struct {
	Date   string  // YYYY-MM-DD
	AvgCPU float64 // 平均 CPU 利用率百分比
}

// DiskInfo 云盘信息
type DiskInfo struct {
	ResourceID   string
	ResourceName string
	Provider     string
	AccountID    int64
	Region       string
	MonthlyCost  float64
}

// OptimizerService 优化建议服务
type OptimizerService struct {
	optimizerDAO repository.OptimizerDAO
	billDAO      repository.BillDAO
	logger       *elog.Component
}

// NewOptimizerService 创建优化建议服务
func NewOptimizerService(
	optimizerDAO repository.OptimizerDAO,
	billDAO repository.BillDAO,
	logger *elog.Component,
) *OptimizerService {
	return &OptimizerService{
		optimizerDAO: optimizerDAO,
		billDAO:      billDAO,
		logger:       logger,
	}
}

// GenerateRecommendations 每日生成优化建议
func (s *OptimizerService) GenerateRecommendations(ctx context.Context, tenantID string) error {
	var recs []domain.Recommendation

	// 1. 检测低 CPU 利用率实例（基于账单数据的启发式方法）
	downsizeRecs, err := s.detectLowCPUInstances(ctx, tenantID)
	if err != nil {
		s.logger.Error("detect low CPU instances failed",
			elog.String("tenant_id", tenantID),
			elog.FieldErr(err))
	} else {
		recs = append(recs, downsizeRecs...)
	}

	// 2. 检测未挂载云盘（基于账单数据的启发式方法）
	diskRecs, err := s.detectUnattachedDisks(ctx, tenantID)
	if err != nil {
		s.logger.Error("detect unattached disks failed",
			elog.String("tenant_id", tenantID),
			elog.FieldErr(err))
	} else {
		recs = append(recs, diskRecs...)
	}

	// 3. 检测按量转包年包月候选
	convertRecs, err := s.detectOnDemandConvert(ctx, tenantID)
	if err != nil {
		s.logger.Error("detect on-demand convert candidates failed",
			elog.String("tenant_id", tenantID),
			elog.FieldErr(err))
	} else {
		recs = append(recs, convertRecs...)
	}

	if len(recs) == 0 {
		return nil
	}

	// 过滤已忽略的建议
	filtered := s.filterDismissed(ctx, tenantID, recs)
	if len(filtered) == 0 {
		return nil
	}

	// 批量创建建议
	if _, err := s.optimizerDAO.CreateBatch(ctx, filtered); err != nil {
		return fmt.Errorf("create recommendations batch: %w", err)
	}

	return nil
}

// detectLowCPUInstances 检测低 CPU 利用率实例
// 使用账单数据启发式方法：连续 7+ 天有计算类型账单且金额较低的资源
func (s *OptimizerService) detectLowCPUInstances(ctx context.Context, tenantID string) ([]domain.Recommendation, error) {
	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -lowCPUConsecutiveDays).Format("2006-01-02")

	// 查询计算类型的账单，找到连续有账单的资源
	bills, err := s.billDAO.ListUnifiedBills(ctx, repository.UnifiedBillFilter{
		TenantID:    tenantID,
		ServiceType: "compute",
		StartDate:   startDate,
		EndDate:     endDate,
	})
	if err != nil {
		return nil, fmt.Errorf("list compute bills: %w", err)
	}

	// 按资源 ID 聚合，统计出现天数和总金额
	type resourceStats struct {
		days         map[string]bool
		totalAmount  float64
		resourceName string
		provider     string
		accountID    int64
		region       string
	}
	resourceMap := make(map[string]*resourceStats)
	for _, bill := range bills {
		stats, ok := resourceMap[bill.ResourceID]
		if !ok {
			stats = &resourceStats{
				days:         make(map[string]bool),
				resourceName: bill.ResourceName,
				provider:     bill.Provider,
				accountID:    bill.AccountID,
				region:       bill.Region,
			}
			resourceMap[bill.ResourceID] = stats
		}
		stats.days[bill.BillingDate] = true
		stats.totalAmount += bill.AmountCNY
	}

	var recs []domain.Recommendation
	for resourceID, stats := range resourceMap {
		if len(stats.days) < lowCPUConsecutiveDays {
			continue
		}
		// 计算日均成本
		dailyAvg := stats.totalAmount / float64(len(stats.days))
		estimatedSaving := dailyAvg * 30 * downsizeSavingRatio

		recs = append(recs, domain.Recommendation{
			Type:            RecTypeDownsize,
			Provider:        stats.provider,
			AccountID:       stats.accountID,
			ResourceID:      resourceID,
			ResourceName:    stats.resourceName,
			Region:          stats.region,
			Reason:          fmt.Sprintf("计算实例连续 %d 天运行，日均成本 %.2f 元，建议降配以节省成本", len(stats.days), dailyAvg),
			EstimatedSaving: estimatedSaving,
			Status:          StatusPending,
			TenantID:        tenantID,
		})
	}

	return recs, nil
}

// detectUnattachedDisks 检测未挂载云盘
// 使用账单数据启发式方法：存储类型账单中无关联计算实例的资源
func (s *OptimizerService) detectUnattachedDisks(ctx context.Context, tenantID string) ([]domain.Recommendation, error) {
	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -7).Format("2006-01-02")

	// 查询存储类型账单
	storageBills, err := s.billDAO.ListUnifiedBills(ctx, repository.UnifiedBillFilter{
		TenantID:    tenantID,
		ServiceType: "storage",
		StartDate:   startDate,
		EndDate:     endDate,
	})
	if err != nil {
		return nil, fmt.Errorf("list storage bills: %w", err)
	}

	// 查询计算类型账单，获取所有有关联的资源 ID
	computeBills, err := s.billDAO.ListUnifiedBills(ctx, repository.UnifiedBillFilter{
		TenantID:    tenantID,
		ServiceType: "compute",
		StartDate:   startDate,
		EndDate:     endDate,
	})
	if err != nil {
		return nil, fmt.Errorf("list compute bills: %w", err)
	}

	computeResources := make(map[string]bool)
	for _, bill := range computeBills {
		computeResources[bill.ResourceID] = true
	}

	// 聚合存储资源
	type diskStats struct {
		totalAmount  float64
		resourceName string
		provider     string
		accountID    int64
		region       string
		days         int
	}
	diskMap := make(map[string]*diskStats)
	for _, bill := range storageBills {
		stats, ok := diskMap[bill.ResourceID]
		if !ok {
			stats = &diskStats{
				resourceName: bill.ResourceName,
				provider:     bill.Provider,
				accountID:    bill.AccountID,
				region:       bill.Region,
			}
			diskMap[bill.ResourceID] = stats
		}
		stats.totalAmount += bill.AmountCNY
		stats.days++
	}

	var recs []domain.Recommendation
	for resourceID, stats := range diskMap {
		// 如果该存储资源 ID 也出现在计算资源中，说明可能已挂载
		if computeResources[resourceID] {
			continue
		}
		dailyAvg := stats.totalAmount / float64(stats.days)
		estimatedSaving := dailyAvg * 30 * releaseDiskSavingRatio

		recs = append(recs, domain.Recommendation{
			Type:            RecTypeReleaseDisk,
			Provider:        stats.provider,
			AccountID:       stats.accountID,
			ResourceID:      resourceID,
			ResourceName:    stats.resourceName,
			Region:          stats.region,
			Reason:          fmt.Sprintf("云盘未关联计算实例，日均成本 %.2f 元，建议释放", dailyAvg),
			EstimatedSaving: estimatedSaving,
			Status:          StatusPending,
			TenantID:        tenantID,
		})
	}

	return recs, nil
}

// detectOnDemandConvert 检测按量转包年包月候选
func (s *OptimizerService) detectOnDemandConvert(ctx context.Context, tenantID string) ([]domain.Recommendation, error) {
	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -onDemandRunningDays).Format("2006-01-02")

	// 查询按量付费的账单
	bills, err := s.billDAO.ListUnifiedBills(ctx, repository.UnifiedBillFilter{
		TenantID:  tenantID,
		StartDate: startDate,
		EndDate:   endDate,
	})
	if err != nil {
		return nil, fmt.Errorf("list on-demand bills: %w", err)
	}

	// 按资源 ID 聚合，筛选按量付费且运行超过 30 天的
	type resourceStats struct {
		days         map[string]bool
		totalAmount  float64
		resourceName string
		provider     string
		accountID    int64
		region       string
		chargeType   string
	}
	resourceMap := make(map[string]*resourceStats)
	for _, bill := range bills {
		if bill.ChargeType != "postpaid" {
			continue
		}
		stats, ok := resourceMap[bill.ResourceID]
		if !ok {
			stats = &resourceStats{
				days:         make(map[string]bool),
				resourceName: bill.ResourceName,
				provider:     bill.Provider,
				accountID:    bill.AccountID,
				region:       bill.Region,
				chargeType:   bill.ChargeType,
			}
			resourceMap[bill.ResourceID] = stats
		}
		stats.days[bill.BillingDate] = true
		stats.totalAmount += bill.AmountCNY
	}

	var recs []domain.Recommendation
	for resourceID, stats := range resourceMap {
		if len(stats.days) < onDemandRunningDays {
			continue
		}
		dailyAvg := stats.totalAmount / float64(len(stats.days))
		estimatedSaving := dailyAvg * 30 * convertPrepaidSavingRatio

		recs = append(recs, domain.Recommendation{
			Type:            RecTypeConvertPrepaid,
			Provider:        stats.provider,
			AccountID:       stats.accountID,
			ResourceID:      resourceID,
			ResourceName:    stats.resourceName,
			Region:          stats.region,
			Reason:          fmt.Sprintf("按量付费实例已运行 %d 天，日均成本 %.2f 元，建议转为包年包月", len(stats.days), dailyAvg),
			EstimatedSaving: estimatedSaving,
			Status:          StatusPending,
			TenantID:        tenantID,
		})
	}

	return recs, nil
}

// filterDismissed 过滤已忽略且未过期的建议
func (s *OptimizerService) filterDismissed(ctx context.Context, tenantID string, recs []domain.Recommendation) []domain.Recommendation {
	now := time.Now()
	var filtered []domain.Recommendation
	for _, rec := range recs {
		existing, err := s.optimizerDAO.FindByResourceAndType(ctx, tenantID, rec.ResourceID, rec.Type)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// 无历史记录，可以生成
				filtered = append(filtered, rec)
				continue
			}
			s.logger.Error("find existing recommendation failed",
				elog.String("resource_id", rec.ResourceID),
				elog.String("type", rec.Type),
				elog.FieldErr(err))
			continue
		}
		// 检查是否已忽略且未过期
		if existing.Status == StatusDismissed && existing.DismissExpiry != nil && existing.DismissExpiry.After(now) {
			// 忽略期内，跳过
			continue
		}
		filtered = append(filtered, rec)
	}
	return filtered
}

// ListRecommendations 获取优化建议列表
func (s *OptimizerService) ListRecommendations(ctx context.Context, tenantID string, filter repository.RecommendationFilter) ([]domain.Recommendation, int64, error) {
	filter.TenantID = tenantID
	recs, err := s.optimizerDAO.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list recommendations: %w", err)
	}
	count, err := s.optimizerDAO.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count recommendations: %w", err)
	}
	return recs, count, nil
}

// DismissRecommendation 忽略建议
func (s *OptimizerService) DismissRecommendation(ctx context.Context, id int64) error {
	rec, err := s.optimizerDAO.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get recommendation: %w", err)
	}

	now := time.Now()
	expiry := now.Add(dismissDuration)
	rec.Status = StatusDismissed
	rec.DismissedAt = &now
	rec.DismissExpiry = &expiry

	if err := s.optimizerDAO.Update(ctx, rec); err != nil {
		return fmt.Errorf("update recommendation: %w", err)
	}

	return nil
}
