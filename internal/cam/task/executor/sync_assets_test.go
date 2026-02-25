package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock: CloudAccountRepository
// ============================================================================

type mockAccountRepo struct {
	mock.Mock
}

func (m *mockAccountRepo) Create(ctx context.Context, account domain.CloudAccount) (int64, error) {
	args := m.Called(ctx, account)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockAccountRepo) GetByID(ctx context.Context, id int64) (domain.CloudAccount, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(domain.CloudAccount), args.Error(1)
}

func (m *mockAccountRepo) GetByName(ctx context.Context, name, tenantID string) (domain.CloudAccount, error) {
	args := m.Called(ctx, name, tenantID)
	return args.Get(0).(domain.CloudAccount), args.Error(1)
}

func (m *mockAccountRepo) List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.CloudAccount), args.Get(1).(int64), args.Error(2)
}

func (m *mockAccountRepo) Update(ctx context.Context, account domain.CloudAccount) error {
	args := m.Called(ctx, account)
	return args.Error(0)
}

func (m *mockAccountRepo) Delete(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockAccountRepo) UpdateStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *mockAccountRepo) UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error {
	args := m.Called(ctx, id, syncTime, assetCount)
	return args.Error(0)
}

func (m *mockAccountRepo) UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error {
	args := m.Called(ctx, id, testTime, status, errorMsg)
	return args.Error(0)
}

// ============================================================================
// Mock: InstanceRepository
// ============================================================================

type mockInstanceRepo struct {
	mock.Mock
}

func (m *mockInstanceRepo) Create(ctx context.Context, instance camdomain.Instance) (int64, error) {
	args := m.Called(ctx, instance)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockInstanceRepo) CreateBatch(ctx context.Context, instances []camdomain.Instance) (int64, error) {
	args := m.Called(ctx, instances)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockInstanceRepo) Update(ctx context.Context, instance camdomain.Instance) error {
	args := m.Called(ctx, instance)
	return args.Error(0)
}

func (m *mockInstanceRepo) GetByID(ctx context.Context, id int64) (camdomain.Instance, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(camdomain.Instance), args.Error(1)
}

func (m *mockInstanceRepo) GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (camdomain.Instance, error) {
	args := m.Called(ctx, tenantID, modelUID, assetID)
	return args.Get(0).(camdomain.Instance), args.Error(1)
}

func (m *mockInstanceRepo) List(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]camdomain.Instance), args.Error(1)
}

func (m *mockInstanceRepo) Count(ctx context.Context, filter camdomain.InstanceFilter) (int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockInstanceRepo) Delete(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockInstanceRepo) DeleteByAccountID(ctx context.Context, accountID int64) error {
	args := m.Called(ctx, accountID)
	return args.Error(0)
}

func (m *mockInstanceRepo) DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error) {
	args := m.Called(ctx, tenantID, modelUID, assetIDs)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockInstanceRepo) ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error) {
	args := m.Called(ctx, tenantID, modelUID, accountID, region)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockInstanceRepo) ListAssetIDsByModelUID(ctx context.Context, tenantID, modelUID string, accountID int64) ([]string, error) {
	args := m.Called(ctx, tenantID, modelUID, accountID)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockInstanceRepo) Upsert(ctx context.Context, instance camdomain.Instance) error {
	args := m.Called(ctx, instance)
	return args.Error(0)
}

func (m *mockInstanceRepo) Search(ctx context.Context, filter camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]camdomain.Instance), args.Get(1).(int64), args.Error(2)
}

// ============================================================================
// Mock: TaskRepository
// ============================================================================

type mockTaskRepo struct {
	mock.Mock
}

func (m *mockTaskRepo) Create(ctx context.Context, task taskx.Task) error {
	args := m.Called(ctx, task)
	return args.Error(0)
}

func (m *mockTaskRepo) GetByID(ctx context.Context, id string) (taskx.Task, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(taskx.Task), args.Error(1)
}

func (m *mockTaskRepo) Update(ctx context.Context, task taskx.Task) error {
	args := m.Called(ctx, task)
	return args.Error(0)
}

func (m *mockTaskRepo) UpdateStatus(ctx context.Context, id string, status taskx.TaskStatus, message string) error {
	args := m.Called(ctx, id, status, message)
	return args.Error(0)
}

func (m *mockTaskRepo) UpdateProgress(ctx context.Context, id string, progress int, message string) error {
	args := m.Called(ctx, id, progress, message)
	return args.Error(0)
}

func (m *mockTaskRepo) List(ctx context.Context, filter taskx.TaskFilter) ([]taskx.Task, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]taskx.Task), args.Error(1)
}

func (m *mockTaskRepo) Count(ctx context.Context, filter taskx.TaskFilter) (int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockTaskRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// ============================================================================
// Mock: CloudAdapter (cloudx.CloudAdapter)
// ============================================================================

type mockCloudAdapter struct {
	mock.Mock
}

func (m *mockCloudAdapter) GetProvider() domain.CloudProvider {
	args := m.Called()
	return args.Get(0).(domain.CloudProvider)
}

func (m *mockCloudAdapter) Asset() cloudx.AssetAdapter                    { return nil }
func (m *mockCloudAdapter) SecurityGroup() cloudx.SecurityGroupAdapter    { return nil }
func (m *mockCloudAdapter) Image() cloudx.ImageAdapter                    { return nil }
func (m *mockCloudAdapter) Disk() cloudx.DiskAdapter                      { return nil }
func (m *mockCloudAdapter) Snapshot() cloudx.SnapshotAdapter              { return nil }
func (m *mockCloudAdapter) NAS() cloudx.NASAdapter                        { return nil }
func (m *mockCloudAdapter) OSS() cloudx.OSSAdapter                        { return nil }
func (m *mockCloudAdapter) Kafka() cloudx.KafkaAdapter                    { return nil }
func (m *mockCloudAdapter) Elasticsearch() cloudx.ElasticsearchAdapter    { return nil }
func (m *mockCloudAdapter) IAM() cloudx.IAMAdapter                        { return nil }
func (m *mockCloudAdapter) ValidateCredentials(ctx context.Context) error { return nil }

func (m *mockCloudAdapter) ECS() cloudx.ECSAdapter {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(cloudx.ECSAdapter)
}

func (m *mockCloudAdapter) RDS() cloudx.RDSAdapter {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(cloudx.RDSAdapter)
}

func (m *mockCloudAdapter) Redis() cloudx.RedisAdapter {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(cloudx.RedisAdapter)
}

func (m *mockCloudAdapter) MongoDB() cloudx.MongoDBAdapter {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(cloudx.MongoDBAdapter)
}

func (m *mockCloudAdapter) VPC() cloudx.VPCAdapter {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(cloudx.VPCAdapter)
}

func (m *mockCloudAdapter) EIP() cloudx.EIPAdapter {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(cloudx.EIPAdapter)
}

// ============================================================================
// Mock: RDSAdapter
// ============================================================================

type mockRDSAdapter struct {
	mock.Mock
}

func (m *mockRDSAdapter) ListInstances(ctx context.Context, region string) ([]types.RDSInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.RDSInstance), args.Error(1)
}

func (m *mockRDSAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.RDSInstance, error) {
	args := m.Called(ctx, region, instanceID)
	return args.Get(0).(*types.RDSInstance), args.Error(1)
}

func (m *mockRDSAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.RDSInstance, error) {
	args := m.Called(ctx, region, instanceIDs)
	return args.Get(0).([]types.RDSInstance), args.Error(1)
}

func (m *mockRDSAdapter) GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error) {
	args := m.Called(ctx, region, instanceID)
	return args.String(0), args.Error(1)
}

func (m *mockRDSAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.RDSInstanceFilter) ([]types.RDSInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.RDSInstance), args.Error(1)
}

// ============================================================================
// Mock: VPCAdapter
// ============================================================================

type mockVPCAdapter struct {
	mock.Mock
}

func (m *mockVPCAdapter) ListInstances(ctx context.Context, region string) ([]types.VPCInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.VPCInstance), args.Error(1)
}

func (m *mockVPCAdapter) GetInstance(ctx context.Context, region, vpcID string) (*types.VPCInstance, error) {
	args := m.Called(ctx, region, vpcID)
	return args.Get(0).(*types.VPCInstance), args.Error(1)
}

func (m *mockVPCAdapter) ListInstancesByIDs(ctx context.Context, region string, vpcIDs []string) ([]types.VPCInstance, error) {
	args := m.Called(ctx, region, vpcIDs)
	return args.Get(0).([]types.VPCInstance), args.Error(1)
}

func (m *mockVPCAdapter) GetInstanceStatus(ctx context.Context, region, vpcID string) (string, error) {
	args := m.Called(ctx, region, vpcID)
	return args.String(0), args.Error(1)
}

func (m *mockVPCAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.VPCInstanceFilter) ([]types.VPCInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.VPCInstance), args.Error(1)
}

// ============================================================================
// Helper: 创建测试 logger 和测试数据
// ============================================================================

func testLogger() *elog.Component {
	return elog.DefaultLogger
}

func testAccount() *domain.CloudAccount {
	return &domain.CloudAccount{
		ID:              100,
		Name:            "test-aliyun",
		Provider:        domain.CloudProviderAliyun,
		AccessKeyID:     "test-ak-1234567890123456",
		AccessKeySecret: "test-sk-1234567890123456",
		Regions:         []string{"cn-hangzhou", "cn-beijing"},
		Status:          domain.CloudAccountStatusActive,
		TenantID:        "tenant-001",
	}
}

func newTestExecutor(instanceRepo *mockInstanceRepo) *SyncAssetsExecutor {
	return &SyncAssetsExecutor{
		instanceRepo: instanceRepo,
		logger:       testLogger(),
	}
}

// ============================================================================
// Tests: SyncAssetsExecutor
// ============================================================================

func TestSyncAssetsExecutor_GetType(t *testing.T) {
	e := &SyncAssetsExecutor{}
	assert.Equal(t, TaskTypeSyncAssets, e.GetType())
	assert.Equal(t, taskx.TaskType("cam:sync_assets"), e.GetType())
}

func TestSyncRegionRDS_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	ctx := context.Background()

	rdsAdapter := new(mockRDSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("RDS").Return(rdsAdapter)

	// 云端返回2个RDS实例
	rdsAdapter.On("ListInstances", ctx, "cn-hangzhou").Return([]types.RDSInstance{
		{
			InstanceID:    "rm-001",
			InstanceName:  "prod-mysql-01",
			Engine:        "mysql",
			EngineVersion: "8.0",
			Status:        "running",
			Region:        "cn-hangzhou",
		},
		{
			InstanceID:    "rm-002",
			InstanceName:  "prod-mysql-02",
			Engine:        "mysql",
			EngineVersion: "5.7",
			Status:        "running",
			Region:        "cn-hangzhou",
		},
	}, nil)

	// 本地没有已有实例
	instanceRepo.On("ListAssetIDsByRegion", ctx, "tenant-001", "aliyun_rds", int64(100), "cn-hangzhou").
		Return([]string{}, nil)

	// Upsert 每个实例
	instanceRepo.On("Upsert", ctx, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_rds" && inst.TenantID == "tenant-001"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)

	synced, err := executor.syncRegionRDS(ctx, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 2, synced)

	instanceRepo.AssertNumberOfCalls(t, "Upsert", 2)
}

func TestSyncRegionRDS_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	ctx := context.Background()

	rdsAdapter := new(mockRDSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("RDS").Return(rdsAdapter)

	// 云端只有1个实例
	rdsAdapter.On("ListInstances", ctx, "cn-hangzhou").Return([]types.RDSInstance{
		{InstanceID: "rm-001", InstanceName: "prod-mysql-01", Region: "cn-hangzhou"},
	}, nil)

	// 本地有2个实例（rm-002 已不存在于云端）
	instanceRepo.On("ListAssetIDsByRegion", ctx, "tenant-001", "aliyun_rds", int64(100), "cn-hangzhou").
		Return([]string{"rm-001", "rm-002"}, nil)

	// 应该删除 rm-002
	instanceRepo.On("DeleteByAssetIDs", ctx, "tenant-001", "aliyun_rds", []string{"rm-002"}).
		Return(int64(1), nil)

	instanceRepo.On("Upsert", ctx, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)

	synced, err := executor.syncRegionRDS(ctx, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)

	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", ctx, "tenant-001", "aliyun_rds", []string{"rm-002"})
}

func TestSyncRegionRDS_AdapterError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	ctx := context.Background()

	rdsAdapter := new(mockRDSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("RDS").Return(rdsAdapter)

	rdsAdapter.On("ListInstances", ctx, "cn-hangzhou").
		Return([]types.RDSInstance(nil), fmt.Errorf("API rate limit exceeded"))

	executor := newTestExecutor(instanceRepo)

	_, err := executor.syncRegionRDS(ctx, cloudAdapter, account, "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取RDS实例失败")
}

func TestSyncRegionRDS_NilAdapter(t *testing.T) {
	account := testAccount()
	ctx := context.Background()

	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("RDS").Return(nil)

	executor := newTestExecutor(nil)

	_, err := executor.syncRegionRDS(ctx, cloudAdapter, account, "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RDS适配器不可用")
}

func TestSyncRegionVPC_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	ctx := context.Background()

	vpcAdapter := new(mockVPCAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("VPC").Return(vpcAdapter)

	vpcAdapter.On("ListInstances", ctx, "cn-hangzhou").Return([]types.VPCInstance{
		{
			VPCID:     "vpc-001",
			VPCName:   "prod-vpc",
			Status:    "Available",
			Region:    "cn-hangzhou",
			CidrBlock: "10.0.0.0/8",
		},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", ctx, "tenant-001", "aliyun_vpc", int64(100), "cn-hangzhou").
		Return([]string{}, nil)

	instanceRepo.On("Upsert", ctx, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_vpc" &&
			inst.AssetID == "vpc-001" &&
			inst.AssetName == "prod-vpc"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)

	synced, err := executor.syncRegionVPC(ctx, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionRDS_UpsertError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	ctx := context.Background()

	rdsAdapter := new(mockRDSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("RDS").Return(rdsAdapter)

	rdsAdapter.On("ListInstances", ctx, "cn-hangzhou").Return([]types.RDSInstance{
		{InstanceID: "rm-001", InstanceName: "prod-mysql-01", Region: "cn-hangzhou"},
		{InstanceID: "rm-002", InstanceName: "prod-mysql-02", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", ctx, "tenant-001", "aliyun_rds", int64(100), "cn-hangzhou").
		Return([]string{}, nil)

	// 第一个 Upsert 失败，第二个成功
	instanceRepo.On("Upsert", ctx, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.AssetID == "rm-001"
	})).Return(fmt.Errorf("db connection error"))

	instanceRepo.On("Upsert", ctx, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.AssetID == "rm-002"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)

	synced, err := executor.syncRegionRDS(ctx, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	// 只有1个成功
	assert.Equal(t, 1, synced)
}

// ============================================================================
// Tests: convert 函数
// ============================================================================

func TestConvertRDSToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	rdsInst := types.RDSInstance{
		InstanceID:       "rm-test-001",
		InstanceName:     "prod-mysql",
		Engine:           "mysql",
		EngineVersion:    "8.0",
		DBInstanceClass:  "rds.mysql.s2.large",
		Status:           "running",
		Region:           "cn-hangzhou",
		Zone:             "cn-hangzhou-a",
		VPCID:            "vpc-001",
		VSwitchID:        "vsw-001",
		ConnectionString: "rm-test-001.mysql.rds.aliyuncs.com",
		Port:             3306,
		Storage:          100,
		StorageType:      "cloud_essd",
		ChargeType:       "PrePaid",
		CreationTime:     "2024-01-01T00:00:00Z",
		ExpiredTime:      "2025-01-01T00:00:00Z",
	}

	result := executor.convertRDSToInstance(rdsInst, account)

	assert.Equal(t, "aliyun_rds", result.ModelUID)
	assert.Equal(t, "rm-test-001", result.AssetID)
	assert.Equal(t, "prod-mysql", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, int64(100), result.AccountID)

	// 验证属性
	assert.Equal(t, "mysql", result.Attributes["engine"])
	assert.Equal(t, "8.0", result.Attributes["engine_version"])
	assert.Equal(t, "running", result.Attributes["status"])
	assert.Equal(t, "cn-hangzhou", result.Attributes["region"])
}

func TestConvertVPCToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	vpcInst := types.VPCInstance{
		VPCID:     "vpc-test-001",
		VPCName:   "prod-vpc",
		Status:    "Available",
		Region:    "cn-hangzhou",
		CidrBlock: "10.0.0.0/8",
		IsDefault: false,
	}

	result := executor.convertVPCToInstance(vpcInst, account)

	assert.Equal(t, "aliyun_vpc", result.ModelUID)
	assert.Equal(t, "vpc-test-001", result.AssetID)
	assert.Equal(t, "prod-vpc", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, int64(100), result.AccountID)
	assert.Equal(t, "10.0.0.0/8", result.Attributes["cidr_block"])
}

func TestConvertECSToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	ecsInst := types.ECSInstance{
		InstanceID:   "i-test-001",
		InstanceName: "web-server-01",
		Status:       "Running",
		Region:       "cn-hangzhou",
		Zone:         "cn-hangzhou-a",
		InstanceType: "ecs.c6.xlarge",
		CPU:          4,
		Memory:       8192,
		OSType:       "linux",
		OSName:       "CentOS 7.9",
		PrivateIP:    "10.0.1.100",
		PublicIP:     "47.100.1.1",
		VPCID:        "vpc-001",
		ChargeType:   "PrePaid",
		Provider:     "aliyun",
	}

	result := executor.convertECSToInstance(ecsInst, account)

	assert.Equal(t, "aliyun_ecs", result.ModelUID)
	assert.Equal(t, "i-test-001", result.AssetID)
	assert.Equal(t, "web-server-01", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, int64(100), result.AccountID)

	// 验证关键属性
	assert.Equal(t, 4, result.Attributes["cpu"])
	assert.Equal(t, 8192, result.Attributes["memory"])
	assert.Equal(t, "linux", result.Attributes["os_type"])
	assert.Equal(t, "10.0.1.100", result.Attributes["private_ip"])
	assert.Equal(t, "47.100.1.1", result.Attributes["public_ip"])
}

func TestConvertEIPToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	eipInst := types.EIPInstance{
		AllocationID: "eip-test-001",
		Name:         "prod-eip",
		IPAddress:    "47.100.1.1",
		Status:       "InUse",
		Region:       "cn-hangzhou",
		Bandwidth:    100,
		InstanceID:   "i-001",
		InstanceType: "EcsInstance",
	}

	result := executor.convertEIPToInstance(eipInst, account)

	assert.Equal(t, "aliyun_eip", result.ModelUID)
	assert.Equal(t, "eip-test-001", result.AssetID)
	assert.Equal(t, "prod-eip", result.AssetName)
	assert.Equal(t, "47.100.1.1", result.Attributes["ip_address"])
	assert.Equal(t, 100, result.Attributes["bandwidth"])
}
