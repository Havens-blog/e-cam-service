package cloudx

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAdapter(t *testing.T) {
	// 注册一个测试适配器（使用不会与真实云厂商冲突的名称）
	testProvider := domain.CloudProvider("test_provider_reg")
	called := false
	RegisterAdapter(testProvider, func(account *domain.CloudAccount) (CloudAdapter, error) {
		called = true
		return nil, nil
	})
	// 测试结束后清理
	defer func() {
		adapterRegistry.mu.Lock()
		delete(adapterRegistry.creators, testProvider)
		adapterRegistry.mu.Unlock()
	}()

	// 验证注册成功
	assert.True(t, IsProviderRegistered(testProvider))

	// 获取创建函数并调用
	creator, err := GetAdapterCreator(testProvider)
	require.NoError(t, err)
	assert.NotNil(t, creator)

	_, _ = creator(nil)
	assert.True(t, called)
}

func TestGetAdapterCreator_UnsupportedProvider(t *testing.T) {
	creator, err := GetAdapterCreator("nonexistent_provider")
	assert.Nil(t, creator)
	assert.ErrorIs(t, err, ErrUnsupportedProvider)
}

func TestIsProviderRegistered(t *testing.T) {
	assert.False(t, IsProviderRegistered("definitely_not_registered"))
}

func TestGetRegisteredProviders(t *testing.T) {
	providers := GetRegisteredProviders()
	assert.NotNil(t, providers)
}

func TestRegisterAdapter_Overwrite(t *testing.T) {
	testProvider := domain.CloudProvider("overwrite_test")
	callCount := 0
	defer func() {
		adapterRegistry.mu.Lock()
		delete(adapterRegistry.creators, testProvider)
		adapterRegistry.mu.Unlock()
	}()

	// 第一次注册
	RegisterAdapter(testProvider, func(account *domain.CloudAccount) (CloudAdapter, error) {
		callCount = 1
		return nil, nil
	})

	// 第二次注册（覆盖）
	RegisterAdapter(testProvider, func(account *domain.CloudAccount) (CloudAdapter, error) {
		callCount = 2
		return nil, nil
	})

	creator, err := GetAdapterCreator(testProvider)
	require.NoError(t, err)
	_, _ = creator(nil)
	assert.Equal(t, 2, callCount, "应该使用最后注册的创建函数")
}
