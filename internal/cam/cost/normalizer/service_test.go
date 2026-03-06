package normalizer

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
)

func newTestService() *NormalizerService {
	return NewNormalizerService(nil, elog.DefaultLogger)
}

func newTestServiceWithRate(rate float64) *NormalizerService {
	return NewNormalizerServiceWithConfig(nil, elog.DefaultLogger, rate)
}

func TestNormalizeOne_BasicAliyun(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderAliyun,
		ServiceType:  "ecs",
		ResourceID:   "i-abc123",
		ResourceName: "test-ecs",
		Region:       "cn-hangzhou",
		Amount:       100.5,
		Currency:     "CNY",
		BillingCycle: "2024-06",
		Tags:         map[string]string{"env": "prod"},
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "aliyun", bill.Provider)
	assert.Equal(t, domain.ServiceTypeCompute, bill.ServiceType)
	assert.Equal(t, "ecs", bill.ServiceTypeName)
	assert.Equal(t, "i-abc123", bill.ResourceID)
	assert.Equal(t, "test-ecs", bill.ResourceName)
	assert.Equal(t, "cn-hangzhou", bill.Region)
	assert.Equal(t, 100.5, bill.Amount)
	assert.Equal(t, "CNY", bill.Currency)
	assert.Equal(t, 100.5, bill.AmountCNY)
	assert.Equal(t, "2024-06-01", bill.BillingDate)
	assert.Equal(t, map[string]string{"env": "prod"}, bill.Tags)
	// BillingStart = 2024-06-01, BillingEnd = 2024-06-30 23:59:59
	assert.Equal(t, 2024, bill.BillingStart.Year())
	assert.Equal(t, 6, int(bill.BillingStart.Month()))
	assert.Equal(t, 1, bill.BillingStart.Day())
	assert.Equal(t, 30, bill.BillingEnd.Day())
}

func TestNormalizeOne_AWSWithExchangeRate(t *testing.T) {
	svc := newTestServiceWithRate(7.0)
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderAWS,
		ServiceType:  "Amazon Elastic Compute Cloud",
		ResourceID:   "i-0abc123",
		ResourceName: "aws-ec2",
		Region:       "us-east-1",
		Amount:       50.0,
		Currency:     "USD",
		BillingCycle: "2024-03",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "aws", bill.Provider)
	assert.Equal(t, domain.ServiceTypeCompute, bill.ServiceType)
	assert.Equal(t, "USD", bill.Currency)
	assert.Equal(t, 50.0, bill.Amount)
	assert.InDelta(t, 350.0, bill.AmountCNY, 0.01)
}

func TestNormalizeOne_HuaweiCNY(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderHuawei,
		ServiceType:  "hws.service.type.ec2",
		ResourceID:   "hw-123",
		Amount:       200.0,
		Currency:     "CNY",
		BillingCycle: "2024-01",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "huawei", bill.Provider)
	assert.Equal(t, domain.ServiceTypeCompute, bill.ServiceType)
	assert.Equal(t, "CNY", bill.Currency)
	assert.Equal(t, 200.0, bill.AmountCNY)
}

func TestNormalizeOne_TencentCNY(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderTencent,
		ServiceType:  "cvm",
		ResourceID:   "ins-abc",
		Amount:       150.0,
		Currency:     "CNY",
		BillingCycle: "2024-02",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "tencent", bill.Provider)
	assert.Equal(t, domain.ServiceTypeCompute, bill.ServiceType)
	assert.Equal(t, "CNY", bill.Currency)
	assert.Equal(t, 150.0, bill.AmountCNY)
}

func TestNormalizeOne_VolcanoCNY(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderVolcano,
		ServiceType:  "tos",
		ResourceID:   "tos-bucket-1",
		Amount:       80.0,
		Currency:     "CNY",
		BillingCycle: "2024-05",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "volcano", bill.Provider)
	assert.Equal(t, domain.ServiceTypeStorage, bill.ServiceType)
	assert.Equal(t, "CNY", bill.Currency)
	assert.Equal(t, 80.0, bill.AmountCNY)
}

func TestNormalizeOne_MissingProvider(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     "",
		ServiceType:  "ecs",
		ResourceID:   "i-123",
		Amount:       10.0,
		BillingCycle: "2024-01",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "unknown", bill.Provider)
}

func TestNormalizeOne_MissingServiceType(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderAliyun,
		ServiceType:  "",
		ResourceID:   "i-123",
		Amount:       10.0,
		BillingCycle: "2024-01",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, domain.ServiceTypeOther, bill.ServiceType)
}

func TestNormalizeOne_MissingResourceID(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderAliyun,
		ServiceType:  "ecs",
		ResourceID:   "",
		Amount:       10.0,
		BillingCycle: "2024-01",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, "unknown", bill.ResourceID)
}

func TestNormalizeOne_UnknownServiceType(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderAliyun,
		ServiceType:  "some_unknown_service",
		ResourceID:   "i-123",
		Amount:       10.0,
		BillingCycle: "2024-01",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	assert.Equal(t, domain.ServiceTypeOther, bill.ServiceType)
	assert.Equal(t, "some_unknown_service", bill.ServiceTypeName)
}

func TestNormalizeOne_EmptyBillingCycle(t *testing.T) {
	svc := newTestService()
	item := billing.RawBillItem{
		Provider:     shareddomain.CloudProviderAliyun,
		ServiceType:  "ecs",
		ResourceID:   "i-123",
		Amount:       10.0,
		BillingCycle: "",
	}

	bill, err := svc.NormalizeOne(item)
	assert.NoError(t, err)
	// Should default to current month
	assert.False(t, bill.BillingStart.IsZero())
	assert.True(t, bill.BillingEnd.After(bill.BillingStart))
}

func TestNormalize_BatchProcessing(t *testing.T) {
	svc := newTestService()
	items := []billing.RawBillItem{
		{
			Provider:     shareddomain.CloudProviderAliyun,
			ServiceType:  "ecs",
			ResourceID:   "i-1",
			Amount:       100.0,
			BillingCycle: "2024-06",
		},
		{
			Provider:     shareddomain.CloudProviderAWS,
			ServiceType:  "Amazon S3",
			ResourceID:   "bucket-1",
			Amount:       50.0,
			Currency:     "USD",
			BillingCycle: "2024-06",
		},
	}

	bills, err := svc.Normalize(context.Background(), items)
	assert.NoError(t, err)
	assert.Len(t, bills, 2)
	assert.Equal(t, "aliyun", bills[0].Provider)
	assert.Equal(t, "aws", bills[1].Provider)
}

func TestServiceTypeMapper_AllProviders(t *testing.T) {
	mapper := NewServiceTypeMapper()

	validTypes := map[string]bool{
		domain.ServiceTypeCompute:    true,
		domain.ServiceTypeStorage:    true,
		domain.ServiceTypeNetwork:    true,
		domain.ServiceTypeDatabase:   true,
		domain.ServiceTypeMiddleware: true,
		domain.ServiceTypeOther:      true,
	}

	tests := []struct {
		provider    shareddomain.CloudProvider
		serviceType string
		expected    string
	}{
		{shareddomain.CloudProviderAliyun, "ecs", domain.ServiceTypeCompute},
		{shareddomain.CloudProviderAliyun, "oss", domain.ServiceTypeStorage},
		{shareddomain.CloudProviderAliyun, "slb", domain.ServiceTypeNetwork},
		{shareddomain.CloudProviderAliyun, "rds", domain.ServiceTypeDatabase},
		{shareddomain.CloudProviderAliyun, "kafka", domain.ServiceTypeMiddleware},
		{shareddomain.CloudProviderAWS, "amazon elastic compute cloud", domain.ServiceTypeCompute},
		{shareddomain.CloudProviderAWS, "amazon simple storage service", domain.ServiceTypeStorage},
		{shareddomain.CloudProviderVolcano, "ecs", domain.ServiceTypeCompute},
		{shareddomain.CloudProviderVolcano, "tos", domain.ServiceTypeStorage},
		{shareddomain.CloudProviderHuawei, "hws.service.type.ec2", domain.ServiceTypeCompute},
		{shareddomain.CloudProviderHuawei, "hws.service.type.obs", domain.ServiceTypeStorage},
		{shareddomain.CloudProviderTencent, "cvm", domain.ServiceTypeCompute},
		{shareddomain.CloudProviderTencent, "cos", domain.ServiceTypeStorage},
		// Unknown type
		{"unknown_provider", "unknown", domain.ServiceTypeOther},
	}

	for _, tt := range tests {
		result := mapper.Map(tt.provider, tt.serviceType)
		assert.Equal(t, tt.expected, result, "provider=%s, serviceType=%s", tt.provider, tt.serviceType)
		assert.True(t, validTypes[result], "result %q should be a valid service type", result)
	}
}

func TestCurrencyConfig_Convert(t *testing.T) {
	cfg := NewCurrencyConfig(7.2)

	// Aliyun → CNY
	r := cfg.Convert(shareddomain.CloudProviderAliyun, 100.0, "CNY")
	assert.Equal(t, "CNY", r.Currency)
	assert.Equal(t, 100.0, r.AmountCNY)

	// AWS → USD with conversion
	r = cfg.Convert(shareddomain.CloudProviderAWS, 100.0, "USD")
	assert.Equal(t, "USD", r.Currency)
	assert.InDelta(t, 720.0, r.AmountCNY, 0.01)

	// Huawei → CNY
	r = cfg.Convert(shareddomain.CloudProviderHuawei, 200.0, "CNY")
	assert.Equal(t, "CNY", r.Currency)
	assert.Equal(t, 200.0, r.AmountCNY)

	// Tencent → CNY
	r = cfg.Convert(shareddomain.CloudProviderTencent, 150.0, "CNY")
	assert.Equal(t, "CNY", r.Currency)
	assert.Equal(t, 150.0, r.AmountCNY)

	// Volcano → CNY
	r = cfg.Convert(shareddomain.CloudProviderVolcano, 80.0, "CNY")
	assert.Equal(t, "CNY", r.Currency)
	assert.Equal(t, 80.0, r.AmountCNY)
}

func TestCurrencyConfig_DefaultRate(t *testing.T) {
	// Zero rate should use default
	cfg := NewCurrencyConfig(0)
	assert.Equal(t, DefaultUSDToCNYRate, cfg.USDToCNYRate)

	// Negative rate should use default
	cfg = NewCurrencyConfig(-1)
	assert.Equal(t, DefaultUSDToCNYRate, cfg.USDToCNYRate)
}

func TestSetExchangeRate(t *testing.T) {
	svc := newTestService()

	err := svc.SetExchangeRate(7.5)
	assert.NoError(t, err)
	assert.Equal(t, 7.5, svc.currencyConfig.USDToCNYRate)

	err = svc.SetExchangeRate(0)
	assert.Error(t, err)

	err = svc.SetExchangeRate(-1)
	assert.Error(t, err)
}

func TestParseBillingCycle(t *testing.T) {
	start, end := parseBillingCycle("2024-02")
	assert.Equal(t, 2024, start.Year())
	assert.Equal(t, 2, int(start.Month()))
	assert.Equal(t, 1, start.Day())
	assert.Equal(t, 29, end.Day()) // 2024 is a leap year
	assert.Equal(t, 23, end.Hour())
	assert.Equal(t, 59, end.Minute())
	assert.Equal(t, 59, end.Second())

	// December
	start, end = parseBillingCycle("2024-12")
	assert.Equal(t, 12, int(start.Month()))
	assert.Equal(t, 31, end.Day())
}
