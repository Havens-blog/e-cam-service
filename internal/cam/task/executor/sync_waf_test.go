package executor

import (
	"context"
	"testing"

	cloudx "github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockWAFAdapter cloudx.WAFAdapter 的测试桩:仅 ListInstances 走 mock.Mock 计数。
type mockWAFAdapter struct {
	mock.Mock
}

func (m *mockWAFAdapter) ListInstances(ctx context.Context, region string) ([]types.WAFInstance, error) {
	args := m.Called(ctx, region)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.WAFInstance), args.Error(1)
}

func (m *mockWAFAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.WAFInstance, error) {
	return nil, nil
}

func (m *mockWAFAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.WAFInstance, error) {
	return nil, nil
}

func (m *mockWAFAdapter) GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error) {
	return "", nil
}

func (m *mockWAFAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	return nil, nil
}

// wafCloudAdapter 包装共享 mockCloudAdapter,仅让 WAF() 返回注入的桩;
// 其余接口方法沿用共享桩实现,避免逐方法复制。
type wafCloudAdapter struct {
	*mockCloudAdapter
	wafAdapter cloudx.WAFAdapter
}

func (m *wafCloudAdapter) WAF() cloudx.WAFAdapter { return m.wafAdapter }

func tencentAccount() *domain.CloudAccount {
	return &domain.CloudAccount{
		ID:              200,
		Name:            "test-tencent",
		Provider:        domain.CloudProviderTencent,
		AccessKeyID:     "test-ak-tencent",
		AccessKeySecret: "test-sk-tencent",
		Regions:         []string{"ap-guangzhou", "ap-shanghai", "ap-beijing"},
		Status:          domain.CloudAccountStatusActive,
		TenantID:        6,
	}
}

// TestSyncRegionWAFTencentGlobalServiceCanonicalRegionGuard 腾讯 WAF 是全局服务:
// 仅在 canonical 地域 ap-guangzhou 执行一次,其余地域跳过,避免 K×全量请求风暴与
// 限流后的"本地行被整批误删"导致列表缺行。
func TestSyncRegionWAFTencentGlobalServiceCanonicalRegionGuard(t *testing.T) {
	c := context.Background()

	t.Run("非canonical地域跳过,不调厂商也不碰仓库", func(t *testing.T) {
		waf := new(mockWAFAdapter)
		adapter := &wafCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), wafAdapter: waf}
		repo := new(mockInstanceRepo)
		executor := newTestExecutor(repo)

		synced, err := executor.syncRegionWAF(c, adapter, tencentAccount(), "ap-shanghai")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		waf.AssertNotCalled(t, "ListInstances", mock.Anything, mock.Anything)
		repo.AssertNotCalled(t, "Upsert", mock.Anything, mock.Anything)
		repo.AssertNotCalled(t, "ListAssetIDsByRegion", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("canonical地域执行一次全量并落库", func(t *testing.T) {
		waf := new(mockWAFAdapter)
		waf.On("ListInstances", c, "ap-guangzhou").Return([]types.WAFInstance{
			{InstanceID: "waf-1", InstanceName: "d.example.com", Region: "ap-guangzhou", Provider: "tencent"},
		}, nil)

		adapter := &wafCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), wafAdapter: waf}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "tencent_waf", int64(200), "ap-guangzhou").
			Return([]string{}, nil)
		repo.On("Upsert", c, mock.Anything).Return(nil)

		executor := newTestExecutor(repo)
		synced, err := executor.syncRegionWAF(c, adapter, tencentAccount(), "ap-guangzhou")
		require.NoError(t, err)
		assert.Equal(t, 1, synced)
		waf.AssertNumberOfCalls(t, "ListInstances", 1)
		repo.AssertNumberOfCalls(t, "Upsert", 1)
	})

	t.Run("非腾讯厂商不受guard影响,任意地域照常同步", func(t *testing.T) {
		waf := new(mockWAFAdapter)
		waf.On("ListInstances", c, "cn-shanghai").Return([]types.WAFInstance{}, nil)

		adapter := &wafCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), wafAdapter: waf}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "aliyun_waf", int64(100), "cn-shanghai").
			Return([]string{}, nil)

		executor := newTestExecutor(repo)
		synced, err := executor.syncRegionWAF(c, adapter, testAccount(), "cn-shanghai")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		waf.AssertNumberOfCalls(t, "ListInstances", 1)
	})
}
