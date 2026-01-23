package aws

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AssetAdapter AWS资产适配器
type AssetAdapter struct {
	account       *domain.CloudAccount
	defaultRegion string
	logger        *elog.Component
}

// NewAssetAdapter 创建AWS资产适配器
func NewAssetAdapter(account *domain.CloudAccount, defaultRegion string, logger *elog.Component) *AssetAdapter {
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	return &AssetAdapter{
		account:       account,
		defaultRegion: defaultRegion,
		logger:        logger,
	}
}

// GetRegions 获取支持的地域列表
// TODO: 实现AWS地域获取
func (a *AssetAdapter) GetRegions(ctx context.Context) ([]types.Region, error) {
	// AWS 常用地域列表（静态返回，后续可改为动态获取）
	regions := []types.Region{
		{ID: "us-east-1", Name: "us-east-1", LocalName: "美国东部(弗吉尼亚北部)", Description: "US East (N. Virginia)"},
		{ID: "us-east-2", Name: "us-east-2", LocalName: "美国东部(俄亥俄)", Description: "US East (Ohio)"},
		{ID: "us-west-1", Name: "us-west-1", LocalName: "美国西部(加利福尼亚北部)", Description: "US West (N. California)"},
		{ID: "us-west-2", Name: "us-west-2", LocalName: "美国西部(俄勒冈)", Description: "US West (Oregon)"},
		{ID: "ap-northeast-1", Name: "ap-northeast-1", LocalName: "亚太地区(东京)", Description: "Asia Pacific (Tokyo)"},
		{ID: "ap-northeast-2", Name: "ap-northeast-2", LocalName: "亚太地区(首尔)", Description: "Asia Pacific (Seoul)"},
		{ID: "ap-southeast-1", Name: "ap-southeast-1", LocalName: "亚太地区(新加坡)", Description: "Asia Pacific (Singapore)"},
		{ID: "ap-southeast-2", Name: "ap-southeast-2", LocalName: "亚太地区(悉尼)", Description: "Asia Pacific (Sydney)"},
		{ID: "eu-west-1", Name: "eu-west-1", LocalName: "欧洲(爱尔兰)", Description: "Europe (Ireland)"},
		{ID: "eu-central-1", Name: "eu-central-1", LocalName: "欧洲(法兰克福)", Description: "Europe (Frankfurt)"},
		{ID: "cn-north-1", Name: "cn-north-1", LocalName: "中国(北京)", Description: "China (Beijing)"},
		{ID: "cn-northwest-1", Name: "cn-northwest-1", LocalName: "中国(宁夏)", Description: "China (Ningxia)"},
	}

	a.logger.Info("获取AWS地域列表成功", elog.Int("count", len(regions)))
	return regions, nil
}

// GetECSInstances 获取EC2实例列表
// TODO: 实现AWS EC2实例获取
func (a *AssetAdapter) GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error) {
	return nil, cloudx.ErrNotImplemented
}
