package normalizer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// NormalizerService 账单标准化服务
type NormalizerService struct {
	serviceTypeMapper *ServiceTypeMapper
	currencyConfig    *CurrencyConfig
	billDAO           repository.BillDAO
	logger            *elog.Component
}

// NewNormalizerService 创建标准化服务（Wire DI 注入）
func NewNormalizerService(
	billDAO repository.BillDAO,
	logger *elog.Component,
) *NormalizerService {
	return &NormalizerService{
		serviceTypeMapper: NewServiceTypeMapper(),
		currencyConfig:    NewCurrencyConfig(DefaultUSDToCNYRate),
		billDAO:           billDAO,
		logger:            logger,
	}
}

// NewNormalizerServiceWithConfig 创建带自定义配置的标准化服务
func NewNormalizerServiceWithConfig(
	billDAO repository.BillDAO,
	logger *elog.Component,
	usdToCNYRate float64,
) *NormalizerService {
	return &NormalizerService{
		serviceTypeMapper: NewServiceTypeMapper(),
		currencyConfig:    NewCurrencyConfig(usdToCNYRate),
		billDAO:           billDAO,
		logger:            logger,
	}
}

// Normalize 将原始账单批量转换为统一账单模型
func (s *NormalizerService) Normalize(ctx context.Context, items []billing.RawBillItem) ([]domain.UnifiedBill, error) {
	bills := make([]domain.UnifiedBill, 0, len(items))
	for i := range items {
		bill, err := s.NormalizeOne(items[i])
		if err != nil {
			s.logger.Warn("normalize item failed, skipping",
				elog.FieldErr(err),
				elog.Int("index", i),
			)
			continue
		}
		bills = append(bills, bill)
	}
	return bills, nil
}

// NormalizeOne 标准化单条账单
func (s *NormalizerService) NormalizeOne(item billing.RawBillItem) (domain.UnifiedBill, error) {
	provider := string(item.Provider)
	if provider == "" {
		s.logger.Warn("missing provider in raw bill item, using 'unknown'",
			elog.String("resource_id", item.ResourceID),
		)
		provider = "unknown"
	}

	// 服务类型映射
	serviceType := s.serviceTypeMapper.Map(item.Provider, item.ServiceType)
	if item.ServiceType == "" {
		s.logger.Warn("missing service type in raw bill item, using 'other'",
			elog.String("provider", provider),
			elog.String("resource_id", item.ResourceID),
		)
		serviceType = domain.ServiceTypeOther
	}

	// 资源 ID 缺失处理
	resourceID := item.ResourceID
	if resourceID == "" {
		s.logger.Warn("missing resource_id in raw bill item, using 'unknown'",
			elog.String("provider", provider),
		)
		resourceID = "unknown"
	}

	// 币种转换
	currResult := s.currencyConfig.Convert(item.Provider, item.Amount, item.Currency)

	// 解析计费周期
	billingStart, billingEnd := parseBillingCycle(item.BillingCycle)

	// BillingDate = BillingStart 格式化为 "YYYY-MM-DD"
	billingDate := billingStart.Format("2006-01-02")

	now := time.Now().Unix()

	bill := domain.UnifiedBill{
		Provider:        provider,
		BillingStart:    billingStart,
		BillingEnd:      billingEnd,
		ServiceType:     serviceType,
		ServiceTypeName: item.ServiceType,
		ResourceID:      resourceID,
		ResourceName:    item.ResourceName,
		Region:          item.Region,
		Amount:          item.Amount,
		Currency:        currResult.Currency,
		AmountCNY:       currResult.AmountCNY,
		Tags:            item.Tags,
		BillingDate:     billingDate,
		CreateTime:      now,
		UpdateTime:      now,
	}

	return bill, nil
}

// parseBillingCycle 解析计费周期字符串 "YYYY-MM" 为起止时间
// BillingStart = 该月第一天 00:00:00 UTC
// BillingEnd = 该月最后一天 23:59:59 UTC
func parseBillingCycle(cycle string) (time.Time, time.Time) {
	cycle = strings.TrimSpace(cycle)
	if cycle == "" {
		now := time.Now().UTC()
		return defaultBillingRange(now)
	}

	t, err := time.Parse("2006-01", cycle)
	if err != nil {
		now := time.Now().UTC()
		return defaultBillingRange(now)
	}

	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0).Add(-time.Second)
	return start, end
}

// defaultBillingRange 返回当前月份的默认计费范围
func defaultBillingRange(now time.Time) (time.Time, time.Time) {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0).Add(-time.Second)
	return start, end
}

// SetExchangeRate 动态更新汇率
func (s *NormalizerService) SetExchangeRate(rate float64) error {
	if rate <= 0 {
		return fmt.Errorf("exchange rate must be positive, got %f", rate)
	}
	s.currencyConfig.USDToCNYRate = rate
	return nil
}

// GetCurrencyConfig 获取当前币种配置（用于测试）
func (s *NormalizerService) GetCurrencyConfig() *CurrencyConfig {
	return s.currencyConfig
}

// GetServiceTypeMapper 获取服务类型映射器（用于测试）
func (s *NormalizerService) GetServiceTypeMapper() *ServiceTypeMapper {
	return s.serviceTypeMapper
}

// ConvertProvider 将 shared domain CloudProvider 转换为 cost domain 字符串
func ConvertProvider(p shareddomain.CloudProvider) string {
	if p == "" {
		return "unknown"
	}
	return string(p)
}
