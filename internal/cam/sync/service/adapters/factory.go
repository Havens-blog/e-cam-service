package adapters

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AdapterFactory 适配器工厂
type AdapterFactory struct {
	logger *elog.Component
}

// NewAdapterFactory 创建适配器工厂
func NewAdapterFactory(logger *elog.Component) *AdapterFactory {
	return &AdapterFactory{
		logger: logger,
	}
}

// CreateAdapter 根据云账号创建适配器
func (f *AdapterFactory) CreateAdapter(account *domain.CloudAccount) (domain.CloudAdapter, error) {
	if account == nil {
		return nil, fmt.Errorf("账号配置不能为空")
	}

	// 验证账号配置
	if err := account.Validate(); err != nil {
		return nil, fmt.Errorf("账号配置验证失败: %w", err)
	}

	// 检查账号是否启用
	if !account.Enabled {
		return nil, domain.ErrAccountDisabled
	}

	// 检查账号是否过期
	if account.IsExpired() {
		return nil, domain.ErrAccountExpired
	}

	// 根据云厂商类型创建适配器
	switch account.Provider {
	case domain.ProviderAliyun:
		return f.createAliyunAdapter(account), nil
	case domain.ProviderAWS:
		return nil, fmt.Errorf("AWS适配器尚未实现")
	case domain.ProviderAzure:
		return nil, fmt.Errorf("Azure适配器尚未实现")
	default:
		return nil, fmt.Errorf("不支持的云厂商: %s", account.Provider)
	}
}

// createAliyunAdapter 创建阿里云适配器
func (f *AdapterFactory) createAliyunAdapter(account *domain.CloudAccount) domain.CloudAdapter {
	config := AliyunConfig{
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.GetDecryptedSecret(),
		DefaultRegion:   account.DefaultRegion,
	}

	return NewAliyunAdapter(config, f.logger)
}

// CreateAdapterByProvider 根据云厂商类型和凭证创建适配器（用于测试）
func (f *AdapterFactory) CreateAdapterByProvider(
	provider domain.CloudProvider,
	accessKeyID string,
	accessKeySecret string,
	defaultRegion string,
) (domain.CloudAdapter, error) {
	switch provider {
	case domain.ProviderAliyun:
		config := AliyunConfig{
			AccessKeyID:     accessKeyID,
			AccessKeySecret: accessKeySecret,
			DefaultRegion:   defaultRegion,
		}
		return NewAliyunAdapter(config, f.logger), nil
	case domain.ProviderAWS:
		return nil, fmt.Errorf("AWS适配器尚未实现")
	case domain.ProviderAzure:
		return nil, fmt.Errorf("Azure适配器尚未实现")
	default:
		return nil, fmt.Errorf("不支持的云厂商: %s", provider)
	}
}
