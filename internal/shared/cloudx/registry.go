package cloudx

import (
	"fmt"
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

var (
	// 全局适配器注册表
	adapterRegistry = &registry{
		creators: make(map[domain.CloudProvider]AdapterCreator),
	}
)

// registry 适配器注册表
type registry struct {
	mu       sync.RWMutex
	creators map[domain.CloudProvider]AdapterCreator
}

// RegisterAdapter 注册适配器创建函数
// 各云厂商包在 init() 中调用此函数注册自己的适配器
func RegisterAdapter(provider domain.CloudProvider, creator AdapterCreator) {
	adapterRegistry.mu.Lock()
	defer adapterRegistry.mu.Unlock()
	adapterRegistry.creators[provider] = creator
}

// GetAdapterCreator 获取适配器创建函数
func GetAdapterCreator(provider domain.CloudProvider) (AdapterCreator, error) {
	adapterRegistry.mu.RLock()
	defer adapterRegistry.mu.RUnlock()

	creator, ok := adapterRegistry.creators[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	return creator, nil
}

// GetRegisteredProviders 获取已注册的云厂商列表
func GetRegisteredProviders() []domain.CloudProvider {
	adapterRegistry.mu.RLock()
	defer adapterRegistry.mu.RUnlock()

	providers := make([]domain.CloudProvider, 0, len(adapterRegistry.creators))
	for provider := range adapterRegistry.creators {
		providers = append(providers, provider)
	}
	return providers
}

// IsProviderRegistered 检查云厂商是否已注册
func IsProviderRegistered(provider domain.CloudProvider) bool {
	adapterRegistry.mu.RLock()
	defer adapterRegistry.mu.RUnlock()
	_, ok := adapterRegistry.creators[provider]
	return ok
}
