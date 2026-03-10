package executor

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/stretchr/testify/assert"
)

func TestConvertRedisToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.RedisInstance{
		InstanceID:    "r-test-001",
		InstanceName:  "prod-redis",
		Status:        "Normal",
		Region:        "cn-hangzhou",
		Zone:          "cn-hangzhou-a",
		EngineVersion: "6.0",
		Architecture:  "cluster",
		NodeType:      "double",
		Capacity:      4096,
		Bandwidth:     96,
		Connections:   20000,
		VPCID:         "vpc-001",
		VSwitchID:     "vsw-001",
		PrivateIP:     "10.0.1.50",
		Port:          6379,
		ChargeType:    "PrePaid",
	}

	result := executor.convertRedisToInstance(inst, account)

	assert.Equal(t, "aliyun_redis", result.ModelUID)
	assert.Equal(t, "r-test-001", result.AssetID)
	assert.Equal(t, "prod-redis", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, int64(100), result.AccountID)
	assert.Equal(t, "6.0", result.Attributes["engine_version"])
	assert.Equal(t, "cluster", result.Attributes["architecture"])
	assert.Equal(t, 4096, result.Attributes["capacity"])
	assert.Equal(t, "cn-hangzhou", result.Attributes["region"])
}

func TestConvertMongoDBToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.MongoDBInstance{
		InstanceID:     "dds-test-001",
		InstanceName:   "prod-mongodb",
		Status:         "Running",
		Region:         "cn-hangzhou",
		Zone:           "cn-hangzhou-a",
		EngineVersion:  "5.0",
		DBInstanceType: "replicate",
		Storage:        100,
		VPCID:          "vpc-001",
		ChargeType:     "PostPaid",
	}

	result := executor.convertMongoDBToInstance(inst, account)

	assert.Equal(t, "aliyun_mongodb", result.ModelUID)
	assert.Equal(t, "dds-test-001", result.AssetID)
	assert.Equal(t, "prod-mongodb", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, int64(100), result.AccountID)
	assert.Equal(t, "5.0", result.Attributes["engine_version"])
	assert.Equal(t, "replicate", result.Attributes["db_instance_type"])
	assert.Equal(t, "cn-hangzhou", result.Attributes["region"])
}

func TestConvertLBToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.LBInstance{
		LoadBalancerID:   "lb-test-001",
		LoadBalancerName: "prod-slb",
		LoadBalancerType: "slb",
		Status:           "active",
		Region:           "cn-hangzhou",
		Address:          "10.0.1.200",
		AddressType:      "intranet",
		VPCID:            "vpc-001",
		Bandwidth:        1000,
	}

	result := executor.convertLBToInstance(inst, account)

	assert.Equal(t, "aliyun_lb", result.ModelUID)
	assert.Equal(t, "lb-test-001", result.AssetID)
	assert.Equal(t, "prod-slb", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "slb", result.Attributes["load_balancer_type"])
	assert.Equal(t, "10.0.1.200", result.Attributes["address"])
}

func TestConvertNASToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.NASInstance{
		FileSystemID:   "nas-test-001",
		FileSystemName: "prod-nas",
		Status:         "Running",
		Region:         "cn-hangzhou",
		Zone:           "cn-hangzhou-a",
		FileSystemType: "standard",
		ProtocolType:   "NFS",
		StorageType:    "Performance",
		Capacity:       1024,
		UsedCapacity:   512,
		VPCID:          "vpc-001",
		ChargeType:     "PayAsYouGo",
		EncryptType:    1,
	}

	result := executor.convertNASToInstance(inst, account)

	assert.Equal(t, "aliyun_nas", result.ModelUID)
	assert.Equal(t, "nas-test-001", result.AssetID)
	assert.Equal(t, "prod-nas", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, int64(100), result.AccountID)
	assert.Equal(t, "standard", result.Attributes["file_system_type"])
	assert.Equal(t, "NFS", result.Attributes["protocol_type"])
	assert.Equal(t, int64(1024), result.Attributes["capacity"])
}

func TestConvertOSSToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	bucket := types.OSSBucket{
		BucketName:   "prod-bucket-001",
		Region:       "cn-hangzhou",
		StorageClass: "Standard",
		ACL:          "private",
		Versioning:   "Enabled",
	}

	result := executor.convertOSSToInstance(bucket, account)

	assert.Equal(t, "aliyun_oss", result.ModelUID)
	assert.Equal(t, "prod-bucket-001", result.AssetID)
	assert.Equal(t, "prod-bucket-001", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "Standard", result.Attributes["storage_class"])
	assert.Equal(t, "private", result.Attributes["acl"])
}

func TestConvertKafkaToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.KafkaInstance{
		InstanceID:   "kafka-test-001",
		InstanceName: "prod-kafka",
		Status:       "running",
		Region:       "cn-hangzhou",
		Zone:         "cn-hangzhou-a",
		Version:      "2.6.0",
		SpecType:     "professional",
		TopicCount:   50,
		DiskSize:     500,
		DiskType:     "cloud_ssd",
		Bandwidth:    120,
		VPCID:        "vpc-001",
		ChargeType:   "PrePaid",
	}

	result := executor.convertKafkaToInstance(inst, account)

	assert.Equal(t, "aliyun_kafka", result.ModelUID)
	assert.Equal(t, "kafka-test-001", result.AssetID)
	assert.Equal(t, "prod-kafka", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "2.6.0", result.Attributes["version"])
	assert.Equal(t, "professional", result.Attributes["spec_type"])
	assert.Equal(t, 50, result.Attributes["topic_count"])
}

func TestConvertElasticsearchToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.ElasticsearchInstance{
		InstanceID:   "es-test-001",
		InstanceName: "prod-es",
		Status:       "active",
		Region:       "cn-hangzhou",
		Version:      "7.10",
		NodeCount:    3,
		NodeSpec:     "elasticsearch.sn2ne.large",
		NodeDiskSize: 100,
		NodeDiskType: "cloud_ssd",
		VPCID:        "vpc-001",
		ChargeType:   "PostPaid",
	}

	result := executor.convertElasticsearchToInstance(inst, account)

	assert.Equal(t, "aliyun_elasticsearch", result.ModelUID)
	assert.Equal(t, "es-test-001", result.AssetID)
	assert.Equal(t, "prod-es", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "7.10", result.Attributes["version"])
	assert.Equal(t, 3, result.Attributes["node_count"])
}

func TestConvertDiskToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.DiskInstance{
		DiskID:             "d-test-001",
		DiskName:           "prod-data-disk",
		DiskType:           "data",
		Category:           "cloud_essd",
		Size:               500,
		Status:             "In_use",
		Encrypted:          true,
		PerformanceLevel:   "PL1",
		InstanceID:         "i-001",
		Device:             "/dev/xvdb",
		DeleteWithInstance: false,
		Region:             "cn-hangzhou",
		Zone:               "cn-hangzhou-a",
		ChargeType:         "PrePaid",
	}

	result := executor.convertDiskToInstance(inst, account)

	assert.Equal(t, "aliyun_disk", result.ModelUID)
	assert.Equal(t, "d-test-001", result.AssetID)
	assert.Equal(t, "prod-data-disk", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "data", result.Attributes["disk_type"])
	assert.Equal(t, "cloud_essd", result.Attributes["category"])
	assert.Equal(t, 500, result.Attributes["size"])
	assert.Equal(t, true, result.Attributes["encrypted"])
}

func TestConvertSnapshotToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.SnapshotInstance{
		SnapshotID:     "s-test-001",
		SnapshotName:   "prod-snapshot",
		SnapshotType:   "user",
		Status:         "accomplished",
		SourceDiskID:   "d-001",
		SourceDiskSize: 100,
		SourceDiskType: "system",
		Encrypted:      false,
		Region:         "cn-hangzhou",
	}

	result := executor.convertSnapshotToInstance(inst, account)

	assert.Equal(t, "aliyun_snapshot", result.ModelUID)
	assert.Equal(t, "s-test-001", result.AssetID)
	assert.Equal(t, "prod-snapshot", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "user", result.Attributes["snapshot_type"])
	assert.Equal(t, "d-001", result.Attributes["source_disk_id"])
	assert.Equal(t, 100, result.Attributes["source_disk_size"])
}

func TestConvertSecurityGroupToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.SecurityGroupInstance{
		SecurityGroupID:   "sg-test-001",
		SecurityGroupName: "prod-sg",
		SecurityGroupType: "normal",
		Description:       "Production security group",
		VPCID:             "vpc-001",
		Region:            "cn-hangzhou",
	}

	result := executor.convertSecurityGroupToInstance(inst, account)

	assert.Equal(t, "aliyun_security_group", result.ModelUID)
	assert.Equal(t, "sg-test-001", result.AssetID)
	assert.Equal(t, "prod-sg", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "normal", result.Attributes["security_group_type"])
	assert.Equal(t, "vpc-001", result.Attributes["vpc_id"])
}

func TestConvertImageToInstance(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.ImageInstance{
		ImageID:         "m-test-001",
		ImageName:       "centos-7.9",
		OSType:          "linux",
		OSName:          "CentOS 7.9 64位",
		Architecture:    "x86_64",
		Platform:        "CentOS",
		ImageOwnerAlias: "system",
		Status:          "Available",
		Size:            40,
		Region:          "cn-hangzhou",
	}

	result := executor.convertImageToInstance(inst, account)

	assert.Equal(t, "aliyun_image", result.ModelUID)
	assert.Equal(t, "m-test-001", result.AssetID)
	assert.Equal(t, "centos-7.9", result.AssetName)
	assert.Equal(t, "tenant-001", result.TenantID)
	assert.Equal(t, "linux", result.Attributes["os_type"])
	assert.Equal(t, "x86_64", result.Attributes["architecture"])
	assert.Equal(t, "system", result.Attributes["image_owner_alias"])
}

func TestConvertLBToInstance_EmptyName(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	inst := types.LBInstance{
		LoadBalancerID:   "lb-test-002",
		LoadBalancerName: "",
		LoadBalancerType: "alb",
		Status:           "active",
		Region:           "cn-hangzhou",
	}

	result := executor.convertLBToInstance(inst, account)

	// When name is empty, should fall back to ID
	assert.Equal(t, "lb-test-002", result.AssetName)
	assert.Equal(t, "lb-test-002", result.AssetID)
}

func TestConvertECSToInstance_WithSecurityGroups(t *testing.T) {
	account := testAccount()
	executor := &SyncAssetsExecutor{}

	ecsInst := types.ECSInstance{
		InstanceID:   "i-sg-001",
		InstanceName: "web-with-sg",
		Status:       "Running",
		Region:       "cn-hangzhou",
		Provider:     "aliyun",
		SecurityGroups: []types.SecurityGroup{
			{ID: "sg-001", Name: "web-sg"},
			{ID: "sg-002", Name: "db-sg"},
		},
	}

	result := executor.convertECSToInstance(ecsInst, account)
	assert.Equal(t, "aliyun_ecs", result.ModelUID)
	assert.Equal(t, "i-sg-001", result.AssetID)

	sgIDs := result.Attributes["security_group_ids"].([]string)
	assert.Len(t, sgIDs, 2)
	assert.Contains(t, sgIDs, "sg-001")
	assert.Contains(t, sgIDs, "sg-002")
}
