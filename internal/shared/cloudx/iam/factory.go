package iam

import (
	"fmt"
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/aliyun"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/aws"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/huawei"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/tencent"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam/volcano"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

type adapterFactory struct {
	adapters map[domain.CloudProvider]CloudIAMAdapter
	mu       sync.RWMutex
	logger   *elog.Component
}

func New(logger *elog.Component) CloudIAMAdapterFactory {
	return &adapterFactory{
		adapters: make(map[domain.CloudProvider]CloudIAMAdapter),
		logger:   logger,
	}
}

func (f *adapterFactory) CreateAdapter(provider domain.CloudProvider) (CloudIAMAdapter, error) {
	f.mu.RLock()
	if adapter, exists := f.adapters[provider]; exists {
		f.mu.RUnlock()
		return adapter, nil
	}
	f.mu.RUnlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	if adapter, exists := f.adapters[provider]; exists {
		return adapter, nil
	}

	var adapter CloudIAMAdapter
	var err error

	switch provider {
	case domain.CloudProviderAliyun:
		adapter, err = f.createAliyunAdapter()
	case domain.CloudProviderAWS:
		adapter, err = f.createAWSAdapter()
	case domain.CloudProviderHuawei:
		adapter, err = f.createHuaweiAdapter()
	case domain.CloudProviderTencent:
		adapter, err = f.createTencentAdapter()
	case domain.CloudProviderVolcano:
		adapter, err = f.createVolcanoAdapter()
	default:
		return nil, fmt.Errorf("不支持的云厂�? %s", provider)
	}

	if err != nil {
		return nil, fmt.Errorf("创建适配器失败败: %w", err)
	}

	f.adapters[provider] = adapter

	f.logger.Info("创建云平台适配器成功功",
		elog.String("provider", string(provider)))

	return adapter, nil
}

func (f *adapterFactory) createAliyunAdapter() (CloudIAMAdapter, error) {
	adapter := aliyun.NewAdapter(f.logger)
	return aliyun.NewAdapterWrapper(adapter), nil
}

func (f *adapterFactory) createAWSAdapter() (CloudIAMAdapter, error) {
	adapter := aws.NewAdapter(f.logger)
	return aws.NewAdapterWrapper(adapter), nil
}

func (f *adapterFactory) createHuaweiAdapter() (CloudIAMAdapter, error) {
	adapter := huawei.NewAdapter(f.logger)
	return huawei.NewAdapterWrapper(adapter), nil
}

func (f *adapterFactory) createTencentAdapter() (CloudIAMAdapter, error) {
	adapter := tencent.NewAdapter(f.logger)
	return tencent.NewAdapterWrapper(adapter), nil
}

func (f *adapterFactory) createVolcanoAdapter() (CloudIAMAdapter, error) {
	adapter := volcano.NewAdapter(f.logger)
	return volcano.NewAdapterWrapper(adapter), nil
}

func (f *adapterFactory) ClearCache() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adapters = make(map[domain.CloudProvider]CloudIAMAdapter)
	f.logger.Info("清空适配器缓存存")
}

func (f *adapterFactory) GetCachedAdapter(provider domain.CloudProvider) (CloudIAMAdapter, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	adapter, exists := f.adapters[provider]
	return adapter, exists
}
