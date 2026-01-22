package adapters

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/gotomicro/ego/core/elog"
	ecs "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2"
)

type HuaweiAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string // 默认地域，用于获取地域列表等全局操作
	logger          *elog.Component
	clients         map[string]*ecs.EcsClient
}

type HuaweiConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	DefaultRegion   string
}

func NewHuaweiAdapter(config HuaweiConfig, logger *elog.Component) *HuaweiAdapter {
	defaultRegion := config.DefaultRegion
	if defaultRegion == "" {
		defaultRegion = "cn-south-1"
	}
	return &HuaweiAdapter{
		accessKeyID:     config.AccessKeyID,
		accessKeySecret: config.AccessKeySecret,
		defaultRegion:   config.DefaultRegion,
	}
}

func (a *HuaweiAdapter) GetProvider() domain.CloudProvider {
	return domain.ProviderHuawei
}
