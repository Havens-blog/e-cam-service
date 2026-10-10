package executor

import (
	"context"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockDDOSAdapter cloudx.DDOSAdapter 的测试桩:仅 ListInstances 走 mock.Mock 计数。
type mockDDOSAdapter struct {
	mock.Mock
}

func (m *mockDDOSAdapter) ListInstances(ctx context.Context, region string) ([]types.DDOSInstance, error) {
	args := m.Called(ctx, region)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.DDOSInstance), args.Error(1)
}

func (m *mockDDOSAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.DDOSInstance, error) {
	return nil, nil
}

func (m *mockDDOSAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.DDOSInstance, error) {
	return nil, nil
}

func (m *mockDDOSAdapter) GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error) {
	return "", nil
}

func (m *mockDDOSAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.DDOSInstanceFilter) ([]types.DDOSInstance, error) {
	return nil, nil
}

// ddosCloudAdapter 包装共享 mockCloudAdapter,仅让 DDOS() 返回注入的桩;
// 其余接口方法沿用共享桩实现,避免逐方法复制。
type ddosCloudAdapter struct {
	*mockCloudAdapter
	ddosAdapter cloudx.DDOSAdapter
}

func (m *ddosCloudAdapter) DDOS() cloudx.DDOSAdapter { return m.ddosAdapter }

func huaweiAccount() *domain.CloudAccount {
	return &domain.CloudAccount{
		ID:              300,
		Name:            "test-huawei",
		Provider:        domain.CloudProviderHuawei,
		AccessKeyID:     "test-ak-huawei",
		AccessKeySecret: "test-sk-huawei",
		Regions:         []string{"cn-north-4", "cn-east-3", "ap-southeast-3"},
		Status:          domain.CloudAccountStatusActive,
		TenantID:        6,
	}
}

func awsAccount() *domain.CloudAccount {
	return &domain.CloudAccount{
		ID:              400,
		Name:            "test-aws",
		Provider:        domain.CloudProviderAWS,
		AccessKeyID:     "test-ak-aws",
		AccessKeySecret: "test-sk-aws",
		Regions:         []string{"us-east-2", "us-west-2"},
		Status:          domain.CloudAccountStatusActive,
		TenantID:        6,
	}
}

// TestSyncRegionDDOSGlobalServiceGuard 全局服务守卫:
// 腾讯/华为/AWS 的 DDoS 产品线是账号级全局服务,只允许在 canonical 地域执行一次。
func TestSyncRegionDDOSGlobalServiceGuard(t *testing.T) {
	c := context.Background()

	t.Run("腾讯非canonical地域跳过,不调厂商也不碰仓库", func(t *testing.T) {
		ddos := new(mockDDOSAdapter)
		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		executor := newTestExecutor(repo)

		synced, err := executor.syncRegionDDOS(c, adapter, tencentAccount(), "ap-shanghai")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		ddos.AssertNotCalled(t, "ListInstances", mock.Anything, mock.Anything)
		repo.AssertNotCalled(t, "Upsert", mock.Anything, mock.Anything)
		repo.AssertNotCalled(t, "ListAssetIDsByRegion", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("腾讯canonical地域执行一次全量并落库", func(t *testing.T) {
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c, "ap-guangzhou").Return([]types.DDOSInstance{
			{InstanceID: "bgp-1", InstanceName: "包-1", Region: "ap-guangzhou", Provider: "tencent", Edition: "高防包"},
		}, nil)

		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "tencent_ddos", int64(200), "ap-guangzhou").
			Return([]string{}, nil)
		repo.On("Upsert", c, mock.Anything).Return(nil)

		executor := newTestExecutor(repo)
		synced, err := executor.syncRegionDDOS(c, adapter, tencentAccount(), "ap-guangzhou")
		require.NoError(t, err)
		assert.Equal(t, 1, synced)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)
		repo.AssertNumberOfCalls(t, "Upsert", 1)
	})

	t.Run("华为仅在账号首个地域执行,其余地域跳过", func(t *testing.T) {
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c, "cn-north-4").Return([]types.DDOSInstance{}, nil)
		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "huawei_ddos", int64(300), "cn-north-4").
			Return([]string{}, nil)

		executor := newTestExecutor(repo)
		synced, err := executor.syncRegionDDOS(c, adapter, huaweiAccount(), "cn-north-4")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)

		// 第二个地域直接跳过,不追加调用
		synced2, err2 := executor.syncRegionDDOS(c, adapter, huaweiAccount(), "cn-east-3")
		require.NoError(t, err2)
		assert.Equal(t, 0, synced2)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)
	})

	t.Run("AWS仅在账号首个地域执行", func(t *testing.T) {
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c, "us-east-2").Return([]types.DDOSInstance{
			{InstanceID: "shield-subscription", InstanceName: "AWS Shield Advanced", Region: "us-east-2", Provider: "aws", Edition: "Advanced"},
		}, nil)

		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "aws_ddos", int64(400), "us-east-2").
			Return([]string{}, nil)
		repo.On("Upsert", c, mock.Anything).Return(nil)

		executor := newTestExecutor(repo)
		synced, err := executor.syncRegionDDOS(c, adapter, awsAccount(), "us-east-2")
		require.NoError(t, err)
		assert.Equal(t, 1, synced)

		// 第二个地域跳过
		synced2, err2 := executor.syncRegionDDOS(c, adapter, awsAccount(), "us-west-2")
		require.NoError(t, err2)
		assert.Equal(t, 0, synced2)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)
	})

	t.Run("阿里云不受guard影响,任意地域照常同步", func(t *testing.T) {
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c, "cn-shanghai").Return([]types.DDOSInstance{}, nil)

		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "aliyun_ddos", int64(100), "cn-shanghai").
			Return([]string{}, nil)

		executor := newTestExecutor(repo)
		synced, err := executor.syncRegionDDOS(c, adapter, testAccount(), "cn-shanghai")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)
	})
}

// TestSyncRegionDDOSAdapterUnavailable 适配器不可用(如火山引擎 SDK 无实例枚举
// API)时静默跳过,不报错、不碰仓库,避免每次同步刷错误日志。
func TestSyncRegionDDOSAdapterUnavailable(t *testing.T) {
	c := context.Background()
	adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: nil}
	repo := new(mockInstanceRepo)
	executor := newTestExecutor(repo)

	synced, err := executor.syncRegionDDOS(c, adapter, tencentAccount(), "ap-guangzhou")
	require.NoError(t, err)
	assert.Equal(t, 0, synced)
	repo.AssertNotCalled(t, "Upsert", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ListAssetIDsByRegion", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestSyncRegionDDOSListErrorFailFast 列表失败必须原样返回错误(不得返回部分
// 列表),否则 diffAndUpsert 会把本地未拉到的那部分产品线行当过期删除。
func TestSyncRegionDDOSListErrorFailFast(t *testing.T) {
	c := context.Background()
	ddos := new(mockDDOSAdapter)
	ddos.On("ListInstances", c, "ap-guangzhou").Return(nil, assert.AnError)

	adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
	repo := new(mockInstanceRepo)
	executor := newTestExecutor(repo)

	_, err := executor.syncRegionDDOS(c, adapter, tencentAccount(), "ap-guangzhou")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "获取DDoS防护实例列表失败")
	repo.AssertNotCalled(t, "Upsert", mock.Anything, mock.Anything)
}

// TestSyncRegionDDOSSkipsEmptyInstanceID 缺失实例ID的条目跳过(如 tencent 高防包
// InstanceDetail 为空),其余照常同步。
func TestSyncRegionDDOSSkipsEmptyInstanceID(t *testing.T) {
	c := context.Background()
	ddos := new(mockDDOSAdapter)
	ddos.On("ListInstances", c, "cn-shanghai").Return([]types.DDOSInstance{
		{InstanceID: "", InstanceName: "缺ID", Provider: "aliyun"},
		{InstanceID: "ddoscoo-1", InstanceName: "高防-1", Provider: "aliyun"},
	}, nil)

	adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
	repo := new(mockInstanceRepo)
	repo.On("ListAssetIDsByRegion", c, int64(6), "aliyun_ddos", int64(100), "cn-shanghai").
		Return([]string{}, nil)
	repo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(repo)
	synced, err := executor.syncRegionDDOS(c, adapter, testAccount(), "cn-shanghai")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	repo.AssertNumberOfCalls(t, "Upsert", 1)
}

// TestConvertDDOSToInstance 转换字段映射完整:全部属性落到 attributes,
// model_uid 为 "{provider}_ddos"。
func TestConvertDDOSToInstance(t *testing.T) {
	executor := newTestExecutor(new(mockInstanceRepo))

	inst := types.DDOSInstance{
		InstanceID:         "bgpip-9",
		InstanceName:       "高防IP-9",
		Status:             "idle",
		Region:             "ap-guangzhou",
		Edition:            "高防IP",
		BasicBandwidth:     300,
		ElasticBandwidth:   500,
		ServiceBandwidth:   100,
		BandwidthUnit:      "Mbps",
		CCQPS:              15000,
		ProtectedIPs:       []string{"1.2.3.4", "5.6.7.8"},
		ProtectedIPCount:   2,
		ChargeType:         "PREPAID",
		AutoRenew:          true,
		CreationTime:       "2025-01-02 03:04:05",
		ExpiredTime:        "2026-01-02 03:04:05",
		ProjectID:          "p-1",
		ResourceGroupID:    "rg-1",
		Tags:               map[string]string{"env": "prod"},
		Description:        "region=ap-guangzhou",
		Provider:           "tencent",
	}

	got := executor.convertDDOSToInstance(inst, tencentAccount())

	assert.Equal(t, "tencent_ddos", got.ModelUID)
	assert.Equal(t, "bgpip-9", got.AssetID)
	assert.Equal(t, "高防IP-9", got.AssetName)
	assert.Equal(t, int64(200), got.AccountID)
	assert.Equal(t, int64(6), got.TenantID)

	attrs := got.Attributes
	assert.Equal(t, "idle", attrs["status"])
	assert.Equal(t, "ap-guangzhou", attrs["region"])
	assert.Equal(t, "高防IP", attrs["edition"])
	assert.Equal(t, int64(300), attrs["basic_bandwidth"])
	assert.Equal(t, int64(500), attrs["elastic_bandwidth"])
	assert.Equal(t, int64(100), attrs["service_bandwidth"])
	assert.Equal(t, "Mbps", attrs["bandwidth_unit"])
	assert.Equal(t, int64(15000), attrs["cc_qps"])
	assert.Equal(t, []string{"1.2.3.4", "5.6.7.8"}, attrs["protected_ips"])
	assert.Equal(t, 2, attrs["protected_ip_count"])
	assert.Equal(t, "PREPAID", attrs["charge_type"])
	assert.Equal(t, true, attrs["auto_renew"])
	assert.Equal(t, "p-1", attrs["project_id"])
	assert.Equal(t, "rg-1", attrs["resource_group_id"])
	assert.Equal(t, map[string]string{"env": "prod"}, attrs["tags"])
	assert.Equal(t, "region=ap-guangzhou", attrs["description"])

	// 断言 camdomain.Instance 结构本身(避免属性键误写导致的静默丢字段)
	var asCamdomain camdomain.Instance = got
	_ = asCamdomain
}

// loopCtx 构造带任务循环地域列表的 ctx,模拟同步任务循环注入的 canonical 信息。
func loopCtx(regionIDs ...string) context.Context {
	return context.WithValue(context.Background(), syncLoopRegionIDsKey{}, regionIDs)
}

// TestSyncRegionDDOSCanonicalFollowsLoopRegions 回归锚点:
// canonical 地域必须跟随"本任务循环实际同步的地域"(ctx 注入),而不是
// account.Regions[0]——两者不一致时旧实现会把整组跳过,表现为同步 0 行 0 错误。
func TestSyncRegionDDOSCanonicalFollowsLoopRegions(t *testing.T) {
	c := loopCtx("ap-shanghai", "ap-beijing")

	t.Run("腾讯:循环不含ap-guangzhou时退回循环首地域执行", func(t *testing.T) {
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c, "ap-shanghai").Return([]types.DDOSInstance{}, nil)

		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c, int64(6), "tencent_ddos", int64(200), "ap-shanghai").
			Return([]string{}, nil)

		executor := newTestExecutor(repo)
		// 循环首地域:执行
		synced, err := executor.syncRegionDDOS(c, adapter, tencentAccount(), "ap-shanghai")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)

		// 非循环首地域:跳过
		synced2, err2 := executor.syncRegionDDOS(c, adapter, tencentAccount(), "ap-beijing")
		require.NoError(t, err2)
		assert.Equal(t, 0, synced2)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)
	})

	t.Run("腾讯:循环含ap-guangzhou时仍优先canonical", func(t *testing.T) {
		c2 := loopCtx("ap-beijing", "ap-guangzhou")
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c2, "ap-guangzhou").Return([]types.DDOSInstance{}, nil)

		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c2, int64(6), "tencent_ddos", int64(200), "ap-guangzhou").
			Return([]string{}, nil)

		executor := newTestExecutor(repo)
		// 循环首地域是 ap-beijing,但含 ap-guangzhou → ap-beijing 跳过
		synced, err := executor.syncRegionDDOS(c2, adapter, tencentAccount(), "ap-beijing")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		ddos.AssertNumberOfCalls(t, "ListInstances", 0)
	})

	t.Run("华为:循环首地域 ≠ account.Regions[0] 时按循环首地域执行", func(t *testing.T) {
		c3 := loopCtx("ap-southeast-3", "cn-east-3") // 与 huaweiAccount().Regions 顺序不同
		ddos := new(mockDDOSAdapter)
		ddos.On("ListInstances", c3, "ap-southeast-3").Return([]types.DDOSInstance{}, nil)

		adapter := &ddosCloudAdapter{mockCloudAdapter: new(mockCloudAdapter), ddosAdapter: ddos}
		repo := new(mockInstanceRepo)
		repo.On("ListAssetIDsByRegion", c3, int64(6), "huawei_ddos", int64(300), "ap-southeast-3").
			Return([]string{}, nil)

		executor := newTestExecutor(repo)
		// account.Regions[0]=cn-north-4,但循环首地域是 ap-southeast-3 → 必须执行
		synced, err := executor.syncRegionDDOS(c3, adapter, huaweiAccount(), "ap-southeast-3")
		require.NoError(t, err)
		assert.Equal(t, 0, synced)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)

		// 循环第二个地域:跳过
		synced2, err2 := executor.syncRegionDDOS(c3, adapter, huaweiAccount(), "cn-east-3")
		require.NoError(t, err2)
		assert.Equal(t, 0, synced2)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)

		// account.Regions[0] 本身不在本次循环里:也不能执行(整组只跑一次)
		synced3, err3 := executor.syncRegionDDOS(c3, adapter, huaweiAccount(), "cn-north-4")
		require.NoError(t, err3)
		assert.Equal(t, 0, synced3)
		ddos.AssertNumberOfCalls(t, "ListInstances", 1)
	})
}