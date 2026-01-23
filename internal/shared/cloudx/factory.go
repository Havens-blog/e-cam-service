package cloudx

import (
	"fmt"
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AdapterFactory 统一适配器工厂
type AdapterFactory struct {
	logger   *elog.Component
	adapters sync.Map // map[string]CloudAdapter, key = provider_accountID
}

// NewAdapterFactory 创建适配器工厂
func NewAdapterFactory(logger *elog.Component) *AdapterFactory {
	if logger == nil {
		logger = elog.DefaultLogger
		if logger == nil {
			logger = elog.EgoLogger
		}
	}
	return &AdapterFactory{
		logger: logger,
	}
}

// CreateAdapter 根据云账号创建适配器
func (f *AdapterFactory) CreateAdapter(account *domain.CloudAccount) (CloudAdapter, error) {
	if account == nil {
		return nil, ErrInvalidConfig
	}

	// 验证账号配置
	if account.AccessKeyID == "" || account.AccessKeySecret == "" {
		return nil, ErrInvalidConfig
	}

	// 检查账号是否启用
	if account.Status != domain.CloudAccountStatusActive {
		return nil, ErrAccountDisabled
	}

	// 生成缓存 key
	cacheKey := fmt.Sprintf("%s_%d", account.Provider, account.ID)

	// 尝试从缓存获取
	if cached, ok := f.adapters.Load(cacheKey); ok {
		return cached.(CloudAdapter), nil
	}

	// 从注册表获取创建函数
	creator, err := GetAdapterCreator(account.Provider)
	if err != nil {
		return nil, err
	}

	// 创建适配器
	adapter, err := creator(account)
	if err != nil {
		return nil, err
	}

	// 缓存适配器
	f.adapters.Store(cacheKey, adapter)

	f.logger.Info("创建云适配器成功",
		elog.String("provider", string(account.Provider)),
		elog.Int64("account_id", account.ID))

	return adapter, nil
}

// ClearCache 清空适配器缓存
func (f *AdapterFactory) ClearCache() {
	f.adapters = sync.Map{}
	f.logger.Info("清空适配器缓存")
}

// ClearAccountCache 清空指定账号的适配器缓存
func (f *AdapterFactory) ClearAccountCache(provider domain.CloudProvider, accountID int64) {
	cacheKey := fmt.Sprintf("%s_%d", provider, accountID)
	f.adapters.Delete(cacheKey)
	f.logger.Info("清空账号适配器缓存",
		elog.String("provider", string(provider)),
		elog.Int64("account_id", accountID))
}
