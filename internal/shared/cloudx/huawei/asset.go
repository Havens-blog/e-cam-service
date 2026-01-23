package huawei

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AssetAdapter 华为云资产适配器
type AssetAdapter struct {
	account       *domain.CloudAccount
	defaultRegion string
	logger        *elog.Component
}

// NewAssetAdapter 创建华为云资产适配器
func NewAssetAdapter(account *domain.CloudAccount, defaultRegion string, logger *elog.Component) *AssetAdapter {
	if defaultRegion == "" {
		defaultRegion = "cn-north-4"
	}
	return &AssetAdapter{
		account:       account,
		defaultRegion: defaultRegion,
		logger:        logger,
	}
}

// GetRegions 获取支持的地域列表
// TODO: 实现华为云地域获取
func (a *AssetAdapter) GetRegions(ctx context.Context) ([]types.Region, error) {
	// 华为云常用地域列表（静态返回，后续可改为动态获取）
	regions := []types.Region{
		{ID: "cn-north-1", Name: "cn-north-1", LocalName: "华北-北京一", Description: "China North 1 (Beijing)"},
		{ID: "cn-north-4", Name: "cn-north-4", LocalName: "华北-北京四", Description: "China North 4 (Beijing)"},
		{ID: "cn-east-2", Name: "cn-east-2", LocalName: "华东-上海二", Description: "China East 2 (Shanghai)"},
		{ID: "cn-east-3", Name: "cn-east-3", LocalName: "华东-上海一", Description: "China East 3 (Shanghai)"},
		{ID: "cn-south-1", Name: "cn-south-1", LocalName: "华南-广州", Description: "China South 1 (Guangzhou)"},
		{ID: "cn-southwest-2", Name: "cn-southwest-2", LocalName: "西南-贵阳一", Description: "China Southwest 2 (Guiyang)"},
		{ID: "ap-southeast-1", Name: "ap-southeast-1", LocalName: "亚太-香港", Description: "Asia Pacific (Hong Kong)"},
		{ID: "ap-southeast-2", Name: "ap-southeast-2", LocalName: "亚太-曼谷", Description: "Asia Pacific (Bangkok)"},
		{ID: "ap-southeast-3", Name: "ap-southeast-3", LocalName: "亚太-新加坡", Description: "Asia Pacific (Singapore)"},
	}

	a.logger.Info("获取华为云地域列表成功", elog.Int("count", len(regions)))
	return regions, nil
}

// GetECSInstances 获取ECS实例列表
// TODO: 实现华为云ECS实例获取
func (a *AssetAdapter) GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error) {
	return nil, cloudx.ErrNotImplemented
}
