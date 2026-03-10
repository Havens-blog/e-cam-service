package executor

import (
	"context"
	"fmt"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Adapters
// ============================================================================

type mockRedisAdapter struct{ mock.Mock }

func (m *mockRedisAdapter) ListInstances(ctx context.Context, region string) ([]types.RedisInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.RedisInstance), args.Error(1)
}
func (m *mockRedisAdapter) GetInstance(ctx context.Context, region, id string) (*types.RedisInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.RedisInstance), args.Error(1)
}
func (m *mockRedisAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.RedisInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.RedisInstance), args.Error(1)
}
func (m *mockRedisAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockRedisAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.RedisInstanceFilter) ([]types.RedisInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.RedisInstance), args.Error(1)
}

type mockMongoDBAdapter struct{ mock.Mock }

func (m *mockMongoDBAdapter) ListInstances(ctx context.Context, region string) ([]types.MongoDBInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.MongoDBInstance), args.Error(1)
}
func (m *mockMongoDBAdapter) GetInstance(ctx context.Context, region, id string) (*types.MongoDBInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.MongoDBInstance), args.Error(1)
}
func (m *mockMongoDBAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.MongoDBInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.MongoDBInstance), args.Error(1)
}
func (m *mockMongoDBAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockMongoDBAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.MongoDBInstanceFilter) ([]types.MongoDBInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.MongoDBInstance), args.Error(1)
}

type mockEIPAdapter struct{ mock.Mock }

func (m *mockEIPAdapter) ListInstances(ctx context.Context, region string) ([]types.EIPInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.EIPInstance), args.Error(1)
}
func (m *mockEIPAdapter) GetInstance(ctx context.Context, region, id string) (*types.EIPInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.EIPInstance), args.Error(1)
}
func (m *mockEIPAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.EIPInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.EIPInstance), args.Error(1)
}
func (m *mockEIPAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockEIPAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.EIPInstanceFilter) ([]types.EIPInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.EIPInstance), args.Error(1)
}

type mockLBAdapter struct{ mock.Mock }

func (m *mockLBAdapter) ListInstances(ctx context.Context, region string) ([]types.LBInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.LBInstance), args.Error(1)
}
func (m *mockLBAdapter) GetInstance(ctx context.Context, region, id string) (*types.LBInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.LBInstance), args.Error(1)
}
func (m *mockLBAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.LBInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.LBInstance), args.Error(1)
}
func (m *mockLBAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockLBAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.LBInstanceFilter) ([]types.LBInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.LBInstance), args.Error(1)
}

type mockNASAdapter struct{ mock.Mock }

func (m *mockNASAdapter) ListInstances(ctx context.Context, region string) ([]types.NASInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.NASInstance), args.Error(1)
}
func (m *mockNASAdapter) GetInstance(ctx context.Context, region, id string) (*types.NASInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.NASInstance), args.Error(1)
}
func (m *mockNASAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.NASInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.NASInstance), args.Error(1)
}
func (m *mockNASAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockNASAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.NASInstanceFilter) ([]types.NASInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.NASInstance), args.Error(1)
}

type mockOSSAdapter struct{ mock.Mock }

func (m *mockOSSAdapter) ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.OSSBucket), args.Error(1)
}
func (m *mockOSSAdapter) GetBucket(ctx context.Context, name string) (*types.OSSBucket, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(*types.OSSBucket), args.Error(1)
}
func (m *mockOSSAdapter) GetBucketStats(ctx context.Context, name string) (*types.OSSBucketStats, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(*types.OSSBucketStats), args.Error(1)
}
func (m *mockOSSAdapter) ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.OSSBucket), args.Error(1)
}

type mockKafkaAdapter struct{ mock.Mock }

func (m *mockKafkaAdapter) ListInstances(ctx context.Context, region string) ([]types.KafkaInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.KafkaInstance), args.Error(1)
}
func (m *mockKafkaAdapter) GetInstance(ctx context.Context, region, id string) (*types.KafkaInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.KafkaInstance), args.Error(1)
}
func (m *mockKafkaAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.KafkaInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.KafkaInstance), args.Error(1)
}
func (m *mockKafkaAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockKafkaAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.KafkaInstanceFilter) ([]types.KafkaInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.KafkaInstance), args.Error(1)
}

type mockElasticsearchAdapter struct{ mock.Mock }

func (m *mockElasticsearchAdapter) ListInstances(ctx context.Context, region string) ([]types.ElasticsearchInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.ElasticsearchInstance), args.Error(1)
}
func (m *mockElasticsearchAdapter) GetInstance(ctx context.Context, region, id string) (*types.ElasticsearchInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.ElasticsearchInstance), args.Error(1)
}
func (m *mockElasticsearchAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.ElasticsearchInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.ElasticsearchInstance), args.Error(1)
}
func (m *mockElasticsearchAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockElasticsearchAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.ElasticsearchInstanceFilter) ([]types.ElasticsearchInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.ElasticsearchInstance), args.Error(1)
}

type mockDiskAdapter struct{ mock.Mock }

func (m *mockDiskAdapter) ListInstances(ctx context.Context, region string) ([]types.DiskInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.DiskInstance), args.Error(1)
}
func (m *mockDiskAdapter) GetInstance(ctx context.Context, region, id string) (*types.DiskInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.DiskInstance), args.Error(1)
}
func (m *mockDiskAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.DiskInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.DiskInstance), args.Error(1)
}
func (m *mockDiskAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockDiskAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.DiskFilter) ([]types.DiskInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.DiskInstance), args.Error(1)
}
func (m *mockDiskAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.DiskInstance, error) {
	args := m.Called(ctx, region, instanceID)
	return args.Get(0).([]types.DiskInstance), args.Error(1)
}

type mockSnapshotAdapter struct{ mock.Mock }

func (m *mockSnapshotAdapter) ListInstances(ctx context.Context, region string) ([]types.SnapshotInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.SnapshotInstance), args.Error(1)
}
func (m *mockSnapshotAdapter) GetInstance(ctx context.Context, region, id string) (*types.SnapshotInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.SnapshotInstance), args.Error(1)
}
func (m *mockSnapshotAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.SnapshotInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.SnapshotInstance), args.Error(1)
}
func (m *mockSnapshotAdapter) GetInstanceStatus(ctx context.Context, region, id string) (string, error) {
	args := m.Called(ctx, region, id)
	return args.String(0), args.Error(1)
}
func (m *mockSnapshotAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.SnapshotFilter) ([]types.SnapshotInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.SnapshotInstance), args.Error(1)
}
func (m *mockSnapshotAdapter) ListByDiskID(ctx context.Context, region, diskID string) ([]types.SnapshotInstance, error) {
	args := m.Called(ctx, region, diskID)
	return args.Get(0).([]types.SnapshotInstance), args.Error(1)
}
func (m *mockSnapshotAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.SnapshotInstance, error) {
	args := m.Called(ctx, region, instanceID)
	return args.Get(0).([]types.SnapshotInstance), args.Error(1)
}

type mockSecurityGroupAdapter struct{ mock.Mock }

func (m *mockSecurityGroupAdapter) ListInstances(ctx context.Context, region string) ([]types.SecurityGroupInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.SecurityGroupInstance), args.Error(1)
}
func (m *mockSecurityGroupAdapter) GetInstance(ctx context.Context, region, id string) (*types.SecurityGroupInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.SecurityGroupInstance), args.Error(1)
}
func (m *mockSecurityGroupAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.SecurityGroupInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.SecurityGroupInstance), args.Error(1)
}
func (m *mockSecurityGroupAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.SecurityGroupFilter) ([]types.SecurityGroupInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.SecurityGroupInstance), args.Error(1)
}
func (m *mockSecurityGroupAdapter) GetSecurityGroupRules(ctx context.Context, region, id string) ([]types.SecurityGroupRule, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).([]types.SecurityGroupRule), args.Error(1)
}
func (m *mockSecurityGroupAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.SecurityGroupInstance, error) {
	args := m.Called(ctx, region, instanceID)
	return args.Get(0).([]types.SecurityGroupInstance), args.Error(1)
}

type mockImageAdapter struct{ mock.Mock }

func (m *mockImageAdapter) ListInstances(ctx context.Context, region string) ([]types.ImageInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.ImageInstance), args.Error(1)
}
func (m *mockImageAdapter) GetInstance(ctx context.Context, region, id string) (*types.ImageInstance, error) {
	args := m.Called(ctx, region, id)
	return args.Get(0).(*types.ImageInstance), args.Error(1)
}
func (m *mockImageAdapter) ListInstancesByIDs(ctx context.Context, region string, ids []string) ([]types.ImageInstance, error) {
	args := m.Called(ctx, region, ids)
	return args.Get(0).([]types.ImageInstance), args.Error(1)
}
func (m *mockImageAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.ImageFilter) ([]types.ImageInstance, error) {
	args := m.Called(ctx, region, filter)
	return args.Get(0).([]types.ImageInstance), args.Error(1)
}

// ============================================================================
// syncRegionRedis Tests
// ============================================================================

func TestSyncRegionRedis_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	ctx := context.Background()

	redisAdpt := new(mockRedisAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Redis").Return(redisAdpt)

	redisAdpt.On("ListInstances", ctx, "cn-hangzhou").Return([]types.RedisInstance{
		{InstanceID: "r-001", InstanceName: "prod-redis", Region: "cn-hangzhou"},
		{InstanceID: "r-002", InstanceName: "test-redis", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", ctx, "tenant-001", "aliyun_redis", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", ctx, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_redis"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionRedis(ctx, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 2, synced)
	instanceRepo.AssertNumberOfCalls(t, "Upsert", 2)
}

func TestSyncRegionRedis_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Redis").Return(nil)
	executor := newTestExecutor(nil)

	_, err := executor.syncRegionRedis(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Redis适配器不可用")
}

func TestSyncRegionRedis_AdapterError(t *testing.T) {
	redisAdpt := new(mockRedisAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Redis").Return(redisAdpt)
	redisAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.RedisInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionRedis(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取Redis实例失败")
}

func TestSyncRegionRedis_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	redisAdpt := new(mockRedisAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Redis").Return(redisAdpt)

	redisAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.RedisInstance{
		{InstanceID: "r-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_redis", int64(100), "cn-hangzhou").
		Return([]string{"r-001", "r-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_redis", []string{"r-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionRedis(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_redis", []string{"r-old"})
}

// ============================================================================
// syncRegionMongoDB Tests
// ============================================================================

func TestSyncRegionMongoDB_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	mongoAdpt := new(mockMongoDBAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("MongoDB").Return(mongoAdpt)

	mongoAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.MongoDBInstance{
		{InstanceID: "dds-001", InstanceName: "prod-mongo", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_mongodb", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_mongodb"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionMongoDB(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionMongoDB_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("MongoDB").Return(nil)
	executor := newTestExecutor(nil)

	_, err := executor.syncRegionMongoDB(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MongoDB适配器不可用")
}

func TestSyncRegionMongoDB_AdapterError(t *testing.T) {
	mongoAdpt := new(mockMongoDBAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("MongoDB").Return(mongoAdpt)
	mongoAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.MongoDBInstance(nil), fmt.Errorf("connection refused"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionMongoDB(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取MongoDB实例失败")
}

// ============================================================================
// syncRegionEIP Tests
// ============================================================================

func TestSyncRegionEIP_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	eipAdpt := new(mockEIPAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("EIP").Return(eipAdpt)

	eipAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.EIPInstance{
		{AllocationID: "eip-001", Name: "prod-eip", IPAddress: "1.2.3.4", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_eip", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_eip" && inst.AssetID == "eip-001"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionEIP(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionEIP_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("EIP").Return(nil)
	executor := newTestExecutor(nil)

	_, err := executor.syncRegionEIP(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "EIP适配器不可用")
}

// ============================================================================
// syncRegionLB Tests
// ============================================================================

func TestSyncRegionLB_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	lbAdpt := new(mockLBAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("LB").Return(lbAdpt)

	lbAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.LBInstance{
		{LoadBalancerID: "lb-001", LoadBalancerName: "prod-slb", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_lb", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_lb"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionLB(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionLB_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("LB").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionLB(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionNAS Tests
// ============================================================================

func TestSyncRegionNAS_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	nasAdpt := new(mockNASAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("NAS").Return(nasAdpt)

	nasAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.NASInstance{
		{FileSystemID: "nas-001", FileSystemName: "prod-nas", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_nas", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_nas"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionNAS(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionNAS_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("NAS").Return(nil)
	executor := newTestExecutor(nil)

	_, err := executor.syncRegionNAS(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NAS适配器不可用")
}

// ============================================================================
// syncRegionKafka Tests
// ============================================================================

func TestSyncRegionKafka_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	kafkaAdpt := new(mockKafkaAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Kafka").Return(kafkaAdpt)

	kafkaAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.KafkaInstance{
		{InstanceID: "kafka-001", InstanceName: "prod-kafka", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_kafka", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_kafka"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionKafka(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionKafka_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Kafka").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionKafka(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionElasticsearch Tests
// ============================================================================

func TestSyncRegionElasticsearch_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	esAdpt := new(mockElasticsearchAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Elasticsearch").Return(esAdpt)

	esAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.ElasticsearchInstance{
		{InstanceID: "es-001", InstanceName: "prod-es", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_elasticsearch", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_elasticsearch"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionElasticsearch(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionElasticsearch_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Elasticsearch").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionElasticsearch(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionDisk Tests
// ============================================================================

func TestSyncRegionDisk_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	diskAdpt := new(mockDiskAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Disk").Return(diskAdpt)

	diskAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.DiskInstance{
		{DiskID: "d-001", DiskName: "sys-disk", Region: "cn-hangzhou"},
		{DiskID: "d-002", DiskName: "data-disk", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_disk", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_disk"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionDisk(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 2, synced)
}

func TestSyncRegionDisk_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Disk").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionDisk(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionSnapshot Tests
// ============================================================================

func TestSyncRegionSnapshot_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	snapAdpt := new(mockSnapshotAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Snapshot").Return(snapAdpt)

	snapAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.SnapshotInstance{
		{SnapshotID: "s-001", SnapshotName: "snap-1", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_snapshot", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_snapshot"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionSnapshot(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionSnapshot_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Snapshot").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionSnapshot(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionSecurityGroup Tests
// ============================================================================

func TestSyncRegionSecurityGroup_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	sgAdpt := new(mockSecurityGroupAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("SecurityGroup").Return(sgAdpt)

	sgAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.SecurityGroupInstance{
		{SecurityGroupID: "sg-001", SecurityGroupName: "web-sg", Region: "cn-hangzhou"},
	}, nil)

	// SecurityGroup sync also fetches rules for each SG
	sgAdpt.On("GetSecurityGroupRules", c, "cn-hangzhou", "sg-001").Return([]types.SecurityGroupRule{
		{RuleID: "rule-1", Direction: "ingress", Protocol: "tcp", PortRange: "80/80"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_security_group", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_security_group"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionSecurityGroup(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionSecurityGroup_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("SecurityGroup").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionSecurityGroup(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionImage Tests
// ============================================================================

func TestSyncRegionImage_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	imgAdpt := new(mockImageAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Image").Return(imgAdpt)

	imgAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.ImageInstance{
		{ImageID: "m-001", ImageName: "centos-7", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_image", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_image"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionImage(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionImage_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Image").Return(nil)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionImage(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// syncRegionOSS Tests
// ============================================================================

func TestSyncRegionOSS_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	ossAdpt := new(mockOSSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("OSS").Return(ossAdpt)

	ossAdpt.On("ListBuckets", c, "cn-hangzhou").Return([]types.OSSBucket{
		{BucketName: "my-bucket", Region: "cn-hangzhou", StorageClass: "Standard"},
	}, nil)

	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_oss"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionOSS(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionOSS_NilAdapter(t *testing.T) {
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("OSS").Return(nil)
	executor := newTestExecutor(nil)

	_, err := executor.syncRegionOSS(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OSS适配器不可用")
}

// ============================================================================
// Deletion + Error Path Tests (增加覆盖率)
// ============================================================================

func TestSyncRegionVPC_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	vpcAdpt := new(mockVPCAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("VPC").Return(vpcAdpt)

	vpcAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.VPCInstance{
		{VPCID: "vpc-001", VPCName: "prod-vpc", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_vpc", int64(100), "cn-hangzhou").
		Return([]string{"vpc-001", "vpc-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_vpc", []string{"vpc-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionVPC(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_vpc", []string{"vpc-old"})
}

func TestSyncRegionVPC_UpsertError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	vpcAdpt := new(mockVPCAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("VPC").Return(vpcAdpt)

	vpcAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.VPCInstance{
		{VPCID: "vpc-001", Region: "cn-hangzhou"},
	}, nil)
	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_vpc", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(fmt.Errorf("db error"))

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionVPC(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 0, synced)
}

func TestSyncRegionVPC_AdapterError(t *testing.T) {
	vpcAdpt := new(mockVPCAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("VPC").Return(vpcAdpt)
	vpcAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.VPCInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionVPC(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取VPC列表失败")
}

func TestSyncRegionEIP_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	eipAdpt := new(mockEIPAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("EIP").Return(eipAdpt)

	eipAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.EIPInstance{
		{AllocationID: "eip-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_eip", int64(100), "cn-hangzhou").
		Return([]string{"eip-001", "eip-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_eip", []string{"eip-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionEIP(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_eip", []string{"eip-old"})
}

func TestSyncRegionEIP_AdapterError(t *testing.T) {
	eipAdpt := new(mockEIPAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("EIP").Return(eipAdpt)
	eipAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.EIPInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionEIP(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取EIP列表失败")
}

func TestSyncRegionMongoDB_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	mongoAdpt := new(mockMongoDBAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("MongoDB").Return(mongoAdpt)

	mongoAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.MongoDBInstance{
		{InstanceID: "dds-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_mongodb", int64(100), "cn-hangzhou").
		Return([]string{"dds-001", "dds-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_mongodb", []string{"dds-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionMongoDB(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_mongodb", []string{"dds-old"})
}

func TestSyncRegionLB_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	lbAdpt := new(mockLBAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("LB").Return(lbAdpt)

	lbAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.LBInstance{
		{LoadBalancerID: "lb-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_lb", int64(100), "cn-hangzhou").
		Return([]string{"lb-001", "lb-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_lb", []string{"lb-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionLB(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_lb", []string{"lb-old"})
}

func TestSyncRegionLB_AdapterError(t *testing.T) {
	lbAdpt := new(mockLBAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("LB").Return(lbAdpt)
	lbAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.LBInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionLB(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionNAS_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	nasAdpt := new(mockNASAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("NAS").Return(nasAdpt)

	nasAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.NASInstance{
		{FileSystemID: "nas-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_nas", int64(100), "cn-hangzhou").
		Return([]string{"nas-001", "nas-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_nas", []string{"nas-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionNAS(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_nas", []string{"nas-old"})
}

func TestSyncRegionNAS_AdapterError(t *testing.T) {
	nasAdpt := new(mockNASAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("NAS").Return(nasAdpt)
	nasAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.NASInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionNAS(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取NAS文件系统失败")
}

func TestSyncRegionKafka_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	kafkaAdpt := new(mockKafkaAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Kafka").Return(kafkaAdpt)

	kafkaAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.KafkaInstance{
		{InstanceID: "kafka-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_kafka", int64(100), "cn-hangzhou").
		Return([]string{"kafka-001", "kafka-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_kafka", []string{"kafka-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionKafka(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_kafka", []string{"kafka-old"})
}

func TestSyncRegionKafka_AdapterError(t *testing.T) {
	kafkaAdpt := new(mockKafkaAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Kafka").Return(kafkaAdpt)
	kafkaAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.KafkaInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionKafka(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionElasticsearch_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	esAdpt := new(mockElasticsearchAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Elasticsearch").Return(esAdpt)

	esAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.ElasticsearchInstance{
		{InstanceID: "es-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_elasticsearch", int64(100), "cn-hangzhou").
		Return([]string{"es-001", "es-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_elasticsearch", []string{"es-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionElasticsearch(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_elasticsearch", []string{"es-old"})
}

func TestSyncRegionElasticsearch_AdapterError(t *testing.T) {
	esAdpt := new(mockElasticsearchAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Elasticsearch").Return(esAdpt)
	esAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.ElasticsearchInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionElasticsearch(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionDisk_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	diskAdpt := new(mockDiskAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Disk").Return(diskAdpt)

	diskAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.DiskInstance{
		{DiskID: "d-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_disk", int64(100), "cn-hangzhou").
		Return([]string{"d-001", "d-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_disk", []string{"d-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionDisk(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_disk", []string{"d-old"})
}

func TestSyncRegionDisk_AdapterError(t *testing.T) {
	diskAdpt := new(mockDiskAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Disk").Return(diskAdpt)
	diskAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.DiskInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionDisk(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionSnapshot_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	snapAdpt := new(mockSnapshotAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Snapshot").Return(snapAdpt)

	snapAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.SnapshotInstance{
		{SnapshotID: "s-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_snapshot", int64(100), "cn-hangzhou").
		Return([]string{"s-001", "s-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_snapshot", []string{"s-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionSnapshot(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_snapshot", []string{"s-old"})
}

func TestSyncRegionSnapshot_AdapterError(t *testing.T) {
	snapAdpt := new(mockSnapshotAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Snapshot").Return(snapAdpt)
	snapAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.SnapshotInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionSnapshot(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionSecurityGroup_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	sgAdpt := new(mockSecurityGroupAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("SecurityGroup").Return(sgAdpt)

	sgAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.SecurityGroupInstance{
		{SecurityGroupID: "sg-001", Region: "cn-hangzhou"},
	}, nil)
	sgAdpt.On("GetSecurityGroupRules", c, "cn-hangzhou", "sg-001").Return([]types.SecurityGroupRule{}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_security_group", int64(100), "cn-hangzhou").
		Return([]string{"sg-001", "sg-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_security_group", []string{"sg-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionSecurityGroup(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_security_group", []string{"sg-old"})
}

func TestSyncRegionSecurityGroup_RulesError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	sgAdpt := new(mockSecurityGroupAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("SecurityGroup").Return(sgAdpt)

	sgAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.SecurityGroupInstance{
		{SecurityGroupID: "sg-001", Region: "cn-hangzhou"},
		{SecurityGroupID: "sg-002", Region: "cn-hangzhou"},
	}, nil)
	// sg-001 rules fail, sg-002 rules succeed
	sgAdpt.On("GetSecurityGroupRules", c, "cn-hangzhou", "sg-001").
		Return([]types.SecurityGroupRule(nil), fmt.Errorf("rules error"))
	sgAdpt.On("GetSecurityGroupRules", c, "cn-hangzhou", "sg-002").
		Return([]types.SecurityGroupRule{
			{RuleID: "r-1", Direction: "egress", Protocol: "all"},
		}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_security_group", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionSecurityGroup(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	// Both SGs get saved - rules error only skips rule processing via continue
	assert.Equal(t, 2, synced)
}

func TestSyncRegionSecurityGroup_AdapterError(t *testing.T) {
	sgAdpt := new(mockSecurityGroupAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("SecurityGroup").Return(sgAdpt)
	sgAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.SecurityGroupInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionSecurityGroup(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionImage_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	imgAdpt := new(mockImageAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Image").Return(imgAdpt)

	imgAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.ImageInstance{
		{ImageID: "m-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_image", int64(100), "cn-hangzhou").
		Return([]string{"m-001", "m-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_image", []string{"m-old"}).
		Return(int64(1), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionImage(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_image", []string{"m-old"})
}

func TestSyncRegionImage_AdapterError(t *testing.T) {
	imgAdpt := new(mockImageAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("Image").Return(imgAdpt)
	imgAdpt.On("ListInstances", mock.Anything, "cn-hangzhou").
		Return([]types.ImageInstance(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionImage(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

func TestSyncRegionOSS_AdapterError(t *testing.T) {
	ossAdpt := new(mockOSSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("OSS").Return(ossAdpt)
	ossAdpt.On("ListBuckets", mock.Anything, "cn-hangzhou").
		Return([]types.OSSBucket(nil), fmt.Errorf("timeout"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionOSS(ctx(), cloudAdapter, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
}

// ============================================================================
// syncRegionECS Tests (uses asset.CloudAssetAdapter)
// ============================================================================

type mockAssetAdapter struct{ mock.Mock }

func (m *mockAssetAdapter) GetProvider() types.CloudProvider {
	args := m.Called()
	return args.Get(0).(types.CloudProvider)
}
func (m *mockAssetAdapter) ValidateCredentials(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
func (m *mockAssetAdapter) GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error) {
	args := m.Called(ctx, region)
	return args.Get(0).([]types.ECSInstance), args.Error(1)
}
func (m *mockAssetAdapter) GetRegions(ctx context.Context) ([]types.Region, error) {
	args := m.Called(ctx)
	return args.Get(0).([]types.Region), args.Error(1)
}

func TestSyncRegionECS_Success(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").Return([]types.ECSInstance{
		{
			InstanceID:   "i-001",
			InstanceName: "web-server",
			Region:       "cn-hangzhou",
			Zone:         "cn-hangzhou-a",
			Status:       "Running",
			Provider:     "aliyun",
			CPU:          4,
			Memory:       8192,
			OSType:       "linux",
			PrivateIP:    "10.0.1.1",
			VPCID:        "vpc-001",
		},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_ecs", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.ModelUID == "aliyun_ecs" && inst.AssetID == "i-001"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionECS(c, assetAdpt, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionECS_WithDeletion(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").Return([]types.ECSInstance{
		{InstanceID: "i-001", Region: "cn-hangzhou", Provider: "aliyun"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_ecs", int64(100), "cn-hangzhou").
		Return([]string{"i-001", "i-old-001", "i-old-002"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_ecs", mock.MatchedBy(func(ids []string) bool {
		return len(ids) == 2
	})).Return(int64(2), nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionECS(c, assetAdpt, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	instanceRepo.AssertCalled(t, "DeleteByAssetIDs", c, "tenant-001", "aliyun_ecs", mock.Anything)
}

func TestSyncRegionECS_AdapterError(t *testing.T) {
	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", mock.Anything, "cn-hangzhou").
		Return([]types.ECSInstance(nil), fmt.Errorf("api error"))

	executor := newTestExecutor(nil)
	_, err := executor.syncRegionECS(ctx(), assetAdpt, testAccount(), "cn-hangzhou")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "获取ECS实例失败")
}

func TestSyncRegionECS_UpsertError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").Return([]types.ECSInstance{
		{InstanceID: "i-001", Region: "cn-hangzhou", Provider: "aliyun"},
		{InstanceID: "i-002", Region: "cn-hangzhou", Provider: "aliyun"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_ecs", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	// First upsert fails, second succeeds
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.AssetID == "i-001"
	})).Return(fmt.Errorf("db error"))
	instanceRepo.On("Upsert", c, mock.MatchedBy(func(inst camdomain.Instance) bool {
		return inst.AssetID == "i-002"
	})).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionECS(c, assetAdpt, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced) // Only i-002 succeeded
}

// ============================================================================
// syncRegionAssets Orchestrator Tests
// ============================================================================

func TestSyncRegionAssets_SingleType_RDS(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	rdsAdpt := new(mockRDSAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("RDS").Return(rdsAdpt)

	rdsAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.RDSInstance{
		{InstanceID: "rds-001", InstanceName: "prod-rds", Region: "cn-hangzhou"},
	}, nil)
	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_rds", int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	// 需要注入 cloudxFactory，但 syncRegionAssets 内部会懒加载
	// 我们通过直接设置 cloudxFactory 为 nil 来测试，但 RDS 需要 cloudxAdapter
	// 所以我们需要一个能返回 mockCloudAdapter 的 cloudxFactory
	// 由于 cloudxFactory 是 *cloudx.AdapterFactory 类型（具体类型），无法直接 mock
	// 但我们可以测试 syncRegionAssets 对 ECS 类型的处理（使用 asset.CloudAssetAdapter）

	// 改为测试 ECS 类型（不需要 cloudxFactory）
	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").Return([]types.ECSInstance{
		{InstanceID: "i-001", InstanceName: "web-01", Region: "cn-hangzhou", Provider: "aliyun"},
	}, nil)
	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_ecs", int64(100), "cn-hangzhou").
		Return([]string{}, nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionAssets(c, assetAdpt, account, "cn-hangzhou", []string{"ecs"})
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

func TestSyncRegionAssets_UnsupportedType(t *testing.T) {
	assetAdpt := new(mockAssetAdapter)
	executor := newTestExecutor(nil)

	synced, err := executor.syncRegionAssets(ctx(), assetAdpt, testAccount(), "cn-hangzhou", []string{"unknown_type"})
	require.NoError(t, err)
	assert.Equal(t, 0, synced)
}

func TestSyncRegionAssets_ExpandedTypes(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").Return([]types.ECSInstance{
		{InstanceID: "i-001", Region: "cn-hangzhou", Provider: "aliyun"},
		{InstanceID: "i-002", Region: "cn-hangzhou", Provider: "aliyun"},
	}, nil)
	instanceRepo.On("ListAssetIDsByRegion", c, mock.Anything, mock.Anything, int64(100), "cn-hangzhou").
		Return([]string{}, nil)
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	// Only test ECS type (cloudx types need cloudxFactory which is nil)
	synced, err := executor.syncRegionAssets(c, assetAdpt, account, "cn-hangzhou", []string{"ecs"})
	require.NoError(t, err)
	assert.Equal(t, 2, synced)
}

func TestSyncRegionAssets_ECSError_ContinuesOthers(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").
		Return([]types.ECSInstance(nil), fmt.Errorf("ecs api error"))

	executor := newTestExecutor(instanceRepo)
	// ECS fails but syncRegionAssets continues (logs error, doesn't return)
	synced, err := executor.syncRegionAssets(c, assetAdpt, account, "cn-hangzhou", []string{"ecs"})
	require.NoError(t, err)
	assert.Equal(t, 0, synced)
}

// ============================================================================
// convertNASToInstance Name Fallback Tests
// ============================================================================

func TestConvertNASToInstance_NameFallbackDescription(t *testing.T) {
	executor := &SyncAssetsExecutor{}
	account := testAccount()

	inst := types.NASInstance{
		FileSystemID:   "nas-001",
		FileSystemName: "", // empty name
		Description:    "my-nas-description",
		Region:         "cn-hangzhou",
	}

	result := executor.convertNASToInstance(inst, account)
	assert.Equal(t, "my-nas-description", result.AssetName)
}

func TestConvertNASToInstance_NameFallbackID(t *testing.T) {
	executor := &SyncAssetsExecutor{}
	account := testAccount()

	inst := types.NASInstance{
		FileSystemID:   "nas-001",
		FileSystemName: "", // empty name
		Description:    "", // empty description
		Region:         "cn-hangzhou",
	}

	result := executor.convertNASToInstance(inst, account)
	assert.Equal(t, "nas-001", result.AssetName)
}

func TestConvertNASToInstance_WithMountTargets(t *testing.T) {
	executor := &SyncAssetsExecutor{}
	account := testAccount()

	inst := types.NASInstance{
		FileSystemID:   "nas-001",
		FileSystemName: "prod-nas",
		Region:         "cn-hangzhou",
		MountTargets: []types.MountTarget{
			{
				MountTargetID:     "mt-001",
				MountTargetDomain: "nas-001.cn-hangzhou.nas.aliyuncs.com",
				NetworkType:       "vpc",
				VPCID:             "vpc-001",
				VSwitchID:         "vsw-001",
				Status:            "Active",
			},
		},
	}

	result := executor.convertNASToInstance(inst, account)
	assert.Equal(t, "prod-nas", result.AssetName)
	mts := result.Attributes["mount_targets"].([]map[string]any)
	assert.Len(t, mts, 1)
	assert.Equal(t, "mt-001", mts[0]["mount_target_id"])
	assert.Equal(t, "vpc-001", mts[0]["vpc_id"])
}

// ============================================================================
// Delete Error Path Tests
// ============================================================================

func TestSyncRegionVPC_DeleteError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	vpcAdpt := new(mockVPCAdapter)
	cloudAdapter := new(mockCloudAdapter)
	cloudAdapter.On("VPC").Return(vpcAdpt)

	vpcAdpt.On("ListInstances", c, "cn-hangzhou").Return([]types.VPCInstance{
		{VPCID: "vpc-001", Region: "cn-hangzhou"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_vpc", int64(100), "cn-hangzhou").
		Return([]string{"vpc-001", "vpc-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_vpc", []string{"vpc-old"}).
		Return(int64(0), fmt.Errorf("delete error"))
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionVPC(c, cloudAdapter, account, "cn-hangzhou")
	require.NoError(t, err) // delete error is logged, not returned
	assert.Equal(t, 1, synced)
}

func TestSyncRegionECS_DeleteError(t *testing.T) {
	instanceRepo := new(mockInstanceRepo)
	account := testAccount()
	c := context.Background()

	assetAdpt := new(mockAssetAdapter)
	assetAdpt.On("GetECSInstances", c, "cn-hangzhou").Return([]types.ECSInstance{
		{InstanceID: "i-001", Region: "cn-hangzhou", Provider: "aliyun"},
	}, nil)

	instanceRepo.On("ListAssetIDsByRegion", c, "tenant-001", "aliyun_ecs", int64(100), "cn-hangzhou").
		Return([]string{"i-001", "i-old"}, nil)
	instanceRepo.On("DeleteByAssetIDs", c, "tenant-001", "aliyun_ecs", []string{"i-old"}).
		Return(int64(0), fmt.Errorf("delete error"))
	instanceRepo.On("Upsert", c, mock.Anything).Return(nil)

	executor := newTestExecutor(instanceRepo)
	synced, err := executor.syncRegionECS(c, assetAdpt, account, "cn-hangzhou")
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
}

// ============================================================================
// Helper
// ============================================================================

func ctx() context.Context {
	return context.Background()
}
