package cloudx

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// CloudAdapter 统一云适配器接口
// 每个云厂商实现此接口，提供资产和IAM管理能力
type CloudAdapter interface {
	// GetProvider 获取云厂商类型
	GetProvider() domain.CloudProvider

	// Asset 获取资产适配器 (通用资产管理，已废弃，建议使用 ECS)
	// Deprecated: 请使用 ECS() 获取云虚拟机适配器
	Asset() AssetAdapter

	// ECS 获取ECS适配器 (云虚拟机专用)
	ECS() ECSAdapter

	// RDS 获取RDS适配器 (云数据库MySQL/PostgreSQL等)
	RDS() RDSAdapter

	// Redis 获取Redis适配器 (云Redis)
	Redis() RedisAdapter

	// MongoDB 获取MongoDB适配器 (云MongoDB)
	MongoDB() MongoDBAdapter

	// VPC 获取VPC适配器 (虚拟私有云)
	VPC() VPCAdapter

	// EIP 获取EIP适配器 (弹性公网IP)
	EIP() EIPAdapter

	// NAS 获取NAS适配器 (文件存储)
	NAS() NASAdapter

	// OSS 获取OSS适配器 (对象存储)
	OSS() OSSAdapter

	// Kafka 获取Kafka适配器 (消息队列)
	Kafka() KafkaAdapter

	// Elasticsearch 获取Elasticsearch适配器 (搜索服务)
	Elasticsearch() ElasticsearchAdapter

	// IAM 获取IAM适配器
	IAM() IAMAdapter

	// ValidateCredentials 验证凭证
	ValidateCredentials(ctx context.Context) error
}

// ============================================================================
// ECSAdapter - 云虚拟机适配器接口 (推荐使用)
// ============================================================================

// ECSAdapter ECS云主机适配器接口
// 专门用于云虚拟机的同步和管理
type ECSAdapter interface {
	// GetRegions 获取支持的地域列表
	GetRegions(ctx context.Context) ([]types.Region, error)

	// ListInstances 获取云主机实例列表
	ListInstances(ctx context.Context, region string) ([]types.ECSInstance, error)

	// GetInstance 获取单个云主机实例详情
	GetInstance(ctx context.Context, region, instanceID string) (*types.ECSInstance, error)

	// ListInstancesByIDs 批量获取云主机实例
	ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.ECSInstance, error)

	// GetInstanceStatus 获取实例状态
	GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取实例列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *ECSInstanceFilter) ([]types.ECSInstance, error)
}

// ECSInstanceFilter ECS实例过滤条件
type ECSInstanceFilter struct {
	// 实例ID列表
	InstanceIDs []string `json:"instance_ids,omitempty"`

	// 实例名称（支持模糊匹配）
	InstanceName string `json:"instance_name,omitempty"`

	// 状态过滤
	Status []string `json:"status,omitempty"`

	// VPC ID
	VPCID string `json:"vpc_id,omitempty"`

	// 可用区
	Zone string `json:"zone,omitempty"`

	// 标签过滤
	Tags map[string]string `json:"tags,omitempty"`

	// 分页
	PageNumber int `json:"page_number,omitempty"`
	PageSize   int `json:"page_size,omitempty"`
}

// ============================================================================
// AssetAdapter - 资产适配器接口 (已废弃)
// ============================================================================

// AssetAdapter 资产适配器接口
// Deprecated: 此接口将逐步废弃，请使用 ECSAdapter 等专用适配器
type AssetAdapter interface {
	// GetRegions 获取支持的地域列表
	GetRegions(ctx context.Context) ([]types.Region, error)

	// GetECSInstances 获取云主机实例列表
	// Deprecated: 请使用 ECSAdapter.ListInstances
	GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error)
}

// ============================================================================
// RDSAdapter - 云数据库适配器接口
// ============================================================================

// RDSAdapter RDS云数据库适配器接口
// 用于MySQL、PostgreSQL、MariaDB、SQL Server等关系型数据库
type RDSAdapter interface {
	// ListInstances 获取RDS实例列表
	ListInstances(ctx context.Context, region string) ([]types.RDSInstance, error)

	// GetInstance 获取单个RDS实例详情
	GetInstance(ctx context.Context, region, instanceID string) (*types.RDSInstance, error)

	// ListInstancesByIDs 批量获取RDS实例
	ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.RDSInstance, error)

	// GetInstanceStatus 获取实例状态
	GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取实例列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.RDSInstanceFilter) ([]types.RDSInstance, error)
}

// ============================================================================
// RedisAdapter - 云Redis适配器接口
// ============================================================================

// RedisAdapter Redis云缓存适配器接口
// 用于Redis缓存服务
type RedisAdapter interface {
	// ListInstances 获取Redis实例列表
	ListInstances(ctx context.Context, region string) ([]types.RedisInstance, error)

	// GetInstance 获取单个Redis实例详情
	GetInstance(ctx context.Context, region, instanceID string) (*types.RedisInstance, error)

	// ListInstancesByIDs 批量获取Redis实例
	ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.RedisInstance, error)

	// GetInstanceStatus 获取实例状态
	GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取实例列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.RedisInstanceFilter) ([]types.RedisInstance, error)
}

// ============================================================================
// MongoDBAdapter - 云MongoDB适配器接口
// ============================================================================

// MongoDBAdapter MongoDB云数据库适配器接口
// 用于MongoDB文档数据库
type MongoDBAdapter interface {
	// ListInstances 获取MongoDB实例列表
	ListInstances(ctx context.Context, region string) ([]types.MongoDBInstance, error)

	// GetInstance 获取单个MongoDB实例详情
	GetInstance(ctx context.Context, region, instanceID string) (*types.MongoDBInstance, error)

	// ListInstancesByIDs 批量获取MongoDB实例
	ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.MongoDBInstance, error)

	// GetInstanceStatus 获取实例状态
	GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取实例列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.MongoDBInstanceFilter) ([]types.MongoDBInstance, error)
}

// ============================================================================
// IAMAdapter - IAM适配器接口
// ============================================================================

// IAMAdapter IAM适配器接口
type IAMAdapter interface {
	// ========== 用户管理 ==========

	// ListUsers 获取用户列表
	ListUsers(ctx context.Context) ([]*domain.CloudUser, error)

	// GetUser 获取用户详情
	GetUser(ctx context.Context, userID string) (*domain.CloudUser, error)

	// GetUserPolicies 获取用户的个人权限策略
	GetUserPolicies(ctx context.Context, userID string) ([]domain.PermissionPolicy, error)

	// CreateUser 创建用户
	CreateUser(ctx context.Context, req *types.CreateUserRequest) (*domain.CloudUser, error)

	// UpdateUserPermissions 更新用户权限
	UpdateUserPermissions(ctx context.Context, userID string, policies []domain.PermissionPolicy) error

	// DeleteUser 删除用户
	DeleteUser(ctx context.Context, userID string) error

	// ========== 用户组管理 ==========

	// ListGroups 获取用户组列表
	ListGroups(ctx context.Context) ([]*domain.UserGroup, error)

	// GetGroup 获取用户组详情
	GetGroup(ctx context.Context, groupID string) (*domain.UserGroup, error)

	// CreateGroup 创建用户组
	CreateGroup(ctx context.Context, req *types.CreateGroupRequest) (*domain.UserGroup, error)

	// UpdateGroupPolicies 更新用户组权限策略
	UpdateGroupPolicies(ctx context.Context, groupID string, policies []domain.PermissionPolicy) error

	// DeleteGroup 删除用户组
	DeleteGroup(ctx context.Context, groupID string) error

	// ListGroupUsers 获取用户组成员列表
	ListGroupUsers(ctx context.Context, groupID string) ([]*domain.CloudUser, error)

	// AddUserToGroup 将用户添加到用户组
	AddUserToGroup(ctx context.Context, groupID string, userID string) error

	// RemoveUserFromGroup 将用户从用户组移除
	RemoveUserFromGroup(ctx context.Context, groupID string, userID string) error

	// ========== 策略管理 ==========

	// ListPolicies 获取权限策略列表
	ListPolicies(ctx context.Context) ([]domain.PermissionPolicy, error)

	// GetPolicy 获取策略详情
	GetPolicy(ctx context.Context, policyID string) (*domain.PermissionPolicy, error)
}

// AdapterCreator 适配器创建函数类型
type AdapterCreator func(account *domain.CloudAccount) (CloudAdapter, error)

// ============================================================================
// VPCAdapter - VPC适配器接口
// ============================================================================

// VPCAdapter VPC虚拟私有云适配器接口
type VPCAdapter interface {
	// ListInstances 获取VPC列表
	ListInstances(ctx context.Context, region string) ([]types.VPCInstance, error)

	// GetInstance 获取单个VPC详情
	GetInstance(ctx context.Context, region, vpcID string) (*types.VPCInstance, error)

	// ListInstancesByIDs 批量获取VPC
	ListInstancesByIDs(ctx context.Context, region string, vpcIDs []string) ([]types.VPCInstance, error)

	// GetInstanceStatus 获取VPC状态
	GetInstanceStatus(ctx context.Context, region, vpcID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取VPC列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.VPCInstanceFilter) ([]types.VPCInstance, error)
}

// ============================================================================
// EIPAdapter - 弹性公网IP适配器接口
// ============================================================================

// EIPAdapter 弹性公网IP适配器接口
type EIPAdapter interface {
	// ListInstances 获取EIP列表
	ListInstances(ctx context.Context, region string) ([]types.EIPInstance, error)

	// GetInstance 获取单个EIP详情
	GetInstance(ctx context.Context, region, allocationID string) (*types.EIPInstance, error)

	// ListInstancesByIDs 批量获取EIP
	ListInstancesByIDs(ctx context.Context, region string, allocationIDs []string) ([]types.EIPInstance, error)

	// GetInstanceStatus 获取EIP状态
	GetInstanceStatus(ctx context.Context, region, allocationID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取EIP列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.EIPInstanceFilter) ([]types.EIPInstance, error)
}

// ============================================================================
// NASAdapter - NAS文件存储适配器接口
// ============================================================================

// NASAdapter NAS文件存储适配器接口
// 用于阿里云NAS、华为云SFS、腾讯云CFS、AWS EFS等
type NASAdapter interface {
	// ListInstances 获取NAS文件系统列表
	ListInstances(ctx context.Context, region string) ([]types.NASInstance, error)

	// GetInstance 获取单个NAS文件系统详情
	GetInstance(ctx context.Context, region, fileSystemID string) (*types.NASInstance, error)

	// ListInstancesByIDs 批量获取NAS文件系统
	ListInstancesByIDs(ctx context.Context, region string, fileSystemIDs []string) ([]types.NASInstance, error)

	// GetInstanceStatus 获取文件系统状态
	GetInstanceStatus(ctx context.Context, region, fileSystemID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取文件系统列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.NASInstanceFilter) ([]types.NASInstance, error)
}

// ============================================================================
// OSSAdapter - OSS对象存储适配器接口
// ============================================================================

// OSSAdapter OSS对象存储适配器接口
// 用于阿里云OSS、华为云OBS、腾讯云COS、AWS S3、火山引擎TOS等
type OSSAdapter interface {
	// ListBuckets 获取存储桶列表 (OSS是全局服务，region参数可选)
	ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error)

	// GetBucket 获取单个存储桶详情
	GetBucket(ctx context.Context, bucketName string) (*types.OSSBucket, error)

	// GetBucketStats 获取存储桶统计信息
	GetBucketStats(ctx context.Context, bucketName string) (*types.OSSBucketStats, error)

	// ListBucketsWithFilter 带过滤条件获取存储桶列表
	ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error)
}

// ============================================================================
// KafkaAdapter - Kafka消息队列适配器接口
// ============================================================================

// KafkaAdapter Kafka消息队列适配器接口
// 用于阿里云Kafka、华为云DMS Kafka、腾讯云CKafka、AWS MSK等
type KafkaAdapter interface {
	// ListInstances 获取Kafka实例列表
	ListInstances(ctx context.Context, region string) ([]types.KafkaInstance, error)

	// GetInstance 获取单个Kafka实例详情
	GetInstance(ctx context.Context, region, instanceID string) (*types.KafkaInstance, error)

	// ListInstancesByIDs 批量获取Kafka实例
	ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.KafkaInstance, error)

	// GetInstanceStatus 获取实例状态
	GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取实例列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.KafkaInstanceFilter) ([]types.KafkaInstance, error)
}

// ============================================================================
// ElasticsearchAdapter - Elasticsearch搜索服务适配器接口
// ============================================================================

// ElasticsearchAdapter Elasticsearch搜索服务适配器接口
// 用于阿里云ES、华为云CSS、腾讯云ES、AWS OpenSearch等
type ElasticsearchAdapter interface {
	// ListInstances 获取Elasticsearch实例列表
	ListInstances(ctx context.Context, region string) ([]types.ElasticsearchInstance, error)

	// GetInstance 获取单个Elasticsearch实例详情
	GetInstance(ctx context.Context, region, instanceID string) (*types.ElasticsearchInstance, error)

	// ListInstancesByIDs 批量获取Elasticsearch实例
	ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.ElasticsearchInstance, error)

	// GetInstanceStatus 获取实例状态
	GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error)

	// ListInstancesWithFilter 带过滤条件获取实例列表
	ListInstancesWithFilter(ctx context.Context, region string, filter *types.ElasticsearchInstanceFilter) ([]types.ElasticsearchInstance, error)
}
