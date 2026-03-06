package normalizer

import (
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

const (
	// CurrencyCNY 人民币
	CurrencyCNY = "CNY"
	// CurrencyUSD 美元
	CurrencyUSD = "USD"
	// DefaultUSDToCNYRate 默认美元兑人民币汇率
	DefaultUSDToCNYRate = 7.2
)

// CurrencyConfig 币种配置与汇率转换
type CurrencyConfig struct {
	// USDToCNYRate 美元兑人民币汇率
	USDToCNYRate float64
}

// NewCurrencyConfig 创建币种配置
func NewCurrencyConfig(usdToCNYRate float64) *CurrencyConfig {
	if usdToCNYRate <= 0 {
		usdToCNYRate = DefaultUSDToCNYRate
	}
	return &CurrencyConfig{
		USDToCNYRate: usdToCNYRate,
	}
}

// CurrencyResult 币种转换结果
type CurrencyResult struct {
	Currency  string
	AmountCNY float64
}

// Convert 根据云厂商和原始金额计算币种和人民币等值金额
func (c *CurrencyConfig) Convert(provider shareddomain.CloudProvider, amount float64, rawCurrency string) CurrencyResult {
	switch provider {
	case shareddomain.CloudProviderAliyun,
		shareddomain.CloudProviderHuawei,
		shareddomain.CloudProviderTencent,
		shareddomain.CloudProviderVolcano:
		return CurrencyResult{
			Currency:  CurrencyCNY,
			AmountCNY: amount,
		}
	case shareddomain.CloudProviderAWS:
		return CurrencyResult{
			Currency:  CurrencyUSD,
			AmountCNY: amount * c.USDToCNYRate,
		}
	default:
		// 未知厂商：保留原始币种，AmountCNY 设为 0
		if rawCurrency == "" {
			rawCurrency = CurrencyCNY
		}
		return CurrencyResult{
			Currency:  rawCurrency,
			AmountCNY: 0,
		}
	}
}
