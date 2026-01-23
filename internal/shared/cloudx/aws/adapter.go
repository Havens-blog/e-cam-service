package aws

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func init() {
	// 注册AWS适配器创建函数
	cloudx.RegisterAdapter(domain.CloudProviderAWS, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return NewAdapter(account)
	})
}

// Adapter AWS统一适配器
type Adapter struct {
	account *domain.CloudAccount
	logger  *elog.Component
	asset   *AssetAdapter
	iam     *IAMAdapter
}

// NewAdapter 创建AWS适配器
func NewAdapter(account *domain.CloudAccount) (*Adapter, error) {
	if account == nil {
		return nil, cloudx.ErrInvalidConfig
	}

	logger := elog.DefaultLogger
	if logger == nil {
		logger = elog.EgoLogger
	}

	// 获取默认地域
	defaultRegion := "us-east-1"
	if len(account.Regions) > 0 {
		defaultRegion = account.Regions[0]
	}

	adapter := &Adapter{
		account: account,
		logger:  logger,
	}

	// 创建资产适配器
	adapter.asset = NewAssetAdapter(account, defaultRegion, logger)

	// 创建IAM适配器
	adapter.iam = NewIAMAdapter(account, logger)

	return adapter, nil
}

// GetProvider 获取云厂商类型
func (a *Adapter) GetProvider() domain.CloudProvider {
	return domain.CloudProviderAWS
}

// Asset 获取资产适配器
func (a *Adapter) Asset() cloudx.AssetAdapter {
	return a.asset
}

// IAM 获取IAM适配器
func (a *Adapter) IAM() cloudx.IAMAdapter {
	return a.iam
}

// ValidateCredentials 验证凭证
func (a *Adapter) ValidateCredentials(ctx context.Context) error {
	// 使用资产适配器验证凭证（获取地域列表）
	_, err := a.asset.GetRegions(ctx)
	if err != nil {
		return fmt.Errorf("AWS凭证验证失败: %w", err)
	}

	a.logger.Info("AWS凭证验证成功",
		elog.Int64("account_id", a.account.ID),
		elog.String("account_name", a.account.Name))

	return nil
}
