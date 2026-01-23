package tencent

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AssetAdapter 腾讯云资产适配器
type AssetAdapter struct {
	account       *domain.CloudAccount
	defaultRegion string
	logger        *elog.Component
}

// NewAssetAdapter 创建腾讯云资产适配器
func NewAssetAdapter(account *domain.CloudAccount, defaultRegion string, logger *elog.Component) *AssetAdapter {
	if defaultRegion == "" {
		defaultRegion = "ap-guangzhou"
	}
	return &AssetAdapter{
		account:       account,
		defaultRegion: defaultRegion,
		logger:        logger,
	}
}

// GetRegions 获取支持的地域列表
func (a *AssetAdapter) GetRegions(ctx context.Context) ([]types.Region, error) {
	// 腾讯云常用地域列表（静态返回，后续可改为动态获取）
	regions := []types.Region{
		{ID: "ap-guangzhou", Name: "ap-guangzhou", LocalName: "华南地区(广州)", Description: "South China (Guangzhou)"},
		{ID: "ap-shanghai", Name: "ap-shanghai", LocalName: "华东地区(上海)", Description: "East China (Shanghai)"},
		{ID: "ap-beijing", Name: "ap-beijing", LocalName: "华北地区(北京)", Description: "North China (Beijing)"},
		{ID: "ap-chengdu", Name: "ap-chengdu", LocalName: "西南地区(成都)", Description: "Southwest China (Chengdu)"},
		{ID: "ap-chongqing", Name: "ap-chongqing", LocalName: "西南地区(重庆)", Description: "Southwest China (Chongqing)"},
		{ID: "ap-nanjing", Name: "ap-nanjing", LocalName: "华东地区(南京)", Description: "East China (Nanjing)"},
		{ID: "ap-hongkong", Name: "ap-hongkong", LocalName: "港澳台地区(香港)", Description: "Hong Kong, Macao and Taiwan (Hong Kong)"},
		{ID: "ap-singapore", Name: "ap-singapore", LocalName: "亚太东南(新加坡)", Description: "Southeast Asia (Singapore)"},
		{ID: "ap-tokyo", Name: "ap-tokyo", LocalName: "亚太东北(东京)", Description: "Northeast Asia (Tokyo)"},
		{ID: "ap-seoul", Name: "ap-seoul", LocalName: "亚太东北(首尔)", Description: "Northeast Asia (Seoul)"},
		{ID: "na-siliconvalley", Name: "na-siliconvalley", LocalName: "美国西部(硅谷)", Description: "Western US (Silicon Valley)"},
		{ID: "eu-frankfurt", Name: "eu-frankfurt", LocalName: "欧洲地区(法兰克福)", Description: "Europe (Frankfurt)"},
	}

	a.logger.Info("获取腾讯云地域列表成功", elog.Int("count", len(regions)))
	return regions, nil
}

// GetECSInstances 获取CVM实例列表
// TODO: 实现腾讯云CVM实例获取
func (a *AssetAdapter) GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error) {
	return nil, cloudx.ErrNotImplemented
}
