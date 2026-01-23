package volcano

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AssetAdapter 火山云资产适配器
type AssetAdapter struct {
	account       *domain.CloudAccount
	defaultRegion string
	logger        *elog.Component
}

// NewAssetAdapter 创建火山云资产适配器
func NewAssetAdapter(account *domain.CloudAccount, defaultRegion string, logger *elog.Component) *AssetAdapter {
	if defaultRegion == "" {
		defaultRegion = "cn-beijing"
	}
	return &AssetAdapter{
		account:       account,
		defaultRegion: defaultRegion,
		logger:        logger,
	}
}

// GetRegions 获取支持的地域列表
func (a *AssetAdapter) GetRegions(ctx context.Context) ([]types.Region, error) {
	// 火山云常用地域列表（静态返回，后续可改为动态获取）
	regions := []types.Region{
		{ID: "cn-beijing", Name: "cn-beijing", LocalName: "华北2(北京)", Description: "China North 2 (Beijing)"},
		{ID: "cn-shanghai", Name: "cn-shanghai", LocalName: "华东2(上海)", Description: "China East 2 (Shanghai)"},
		{ID: "cn-guangzhou", Name: "cn-guangzhou", LocalName: "华南1(广州)", Description: "China South 1 (Guangzhou)"},
		{ID: "ap-southeast-1", Name: "ap-southeast-1", LocalName: "亚太东南(柔佛)", Description: "Asia Pacific Southeast (Johor)"},
	}

	a.logger.Info("获取火山云地域列表成功", elog.Int("count", len(regions)))
	return regions, nil
}

// GetECSInstances 获取ECS实例列表
// TODO: 实现火山云ECS实例获取
func (a *AssetAdapter) GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error) {
	return nil, cloudx.ErrNotImplemented
}
