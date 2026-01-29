package aliyun

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func init() {
	// 注册阿里云适配器创建函数
	cloudx.RegisterAdapter(domain.CloudProviderAliyun, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return NewAdapter(account)
	})
}

// Adapter 阿里云统一适配器
type Adapter struct {
	account *domain.CloudAccount
	logger  *elog.Component
	asset   *AssetAdapter
	ecs     *ECSAdapter
	rds     *RDSAdapter
	redis   *RedisAdapter
	mongodb *MongoDBAdapter
	iam     *IAMAdapter
}

// NewAdapter 创建阿里云适配器
func NewAdapter(account *domain.CloudAccount) (*Adapter, error) {
	if account == nil {
		return nil, cloudx.ErrInvalidConfig
	}

	logger := elog.DefaultLogger
	if logger == nil {
		logger = elog.EgoLogger
	}

	// 获取默认地域
	defaultRegion := "cn-hangzhou"
	if len(account.Regions) > 0 {
		defaultRegion = account.Regions[0]
	}

	adapter := &Adapter{
		account: account,
		logger:  logger,
	}

	// 创建资产适配器 (已废弃，保留兼容)
	adapter.asset = NewAssetAdapter(
		account.AccessKeyID,
		account.AccessKeySecret,
		defaultRegion,
		logger,
	)

	// 创建ECS适配器 (推荐使用)
	adapter.ecs = NewECSAdapter(
		account.AccessKeyID,
		account.AccessKeySecret,
		defaultRegion,
		logger,
	)

	// 创建RDS适配器
	adapter.rds = NewRDSAdapter(
		account.AccessKeyID,
		account.AccessKeySecret,
		defaultRegion,
		logger,
	)

	// 创建Redis适配器
	adapter.redis = NewRedisAdapter(
		account.AccessKeyID,
		account.AccessKeySecret,
		defaultRegion,
		logger,
	)

	// 创建MongoDB适配器
	adapter.mongodb = NewMongoDBAdapter(
		account.AccessKeyID,
		account.AccessKeySecret,
		defaultRegion,
		logger,
	)

	// 创建IAM适配器
	adapter.iam = NewIAMAdapter(account, logger)

	return adapter, nil
}

// GetProvider 获取云厂商类型
func (a *Adapter) GetProvider() domain.CloudProvider {
	return domain.CloudProviderAliyun
}

// Asset 获取资产适配器
// Deprecated: 请使用 ECS() 获取云虚拟机适配器
func (a *Adapter) Asset() cloudx.AssetAdapter {
	return a.asset
}

// ECS 获取ECS适配器
func (a *Adapter) ECS() cloudx.ECSAdapter {
	return a.ecs
}

// RDS 获取RDS适配器
func (a *Adapter) RDS() cloudx.RDSAdapter {
	return a.rds
}

// Redis 获取Redis适配器
func (a *Adapter) Redis() cloudx.RedisAdapter {
	return a.redis
}

// MongoDB 获取MongoDB适配器
func (a *Adapter) MongoDB() cloudx.MongoDBAdapter {
	return a.mongodb
}

// IAM 获取IAM适配器
func (a *Adapter) IAM() cloudx.IAMAdapter {
	return a.iam
}

// ValidateCredentials 验证凭证
func (a *Adapter) ValidateCredentials(ctx context.Context) error {
	// 使用ECS适配器验证凭证（获取地域列表）
	_, err := a.ecs.GetRegions(ctx)
	if err != nil {
		return fmt.Errorf("阿里云凭证验证失败: %w", err)
	}

	a.logger.Info("阿里云凭证验证成功",
		elog.Int64("account_id", a.account.ID),
		elog.String("account_name", a.account.Name))

	return nil
}
