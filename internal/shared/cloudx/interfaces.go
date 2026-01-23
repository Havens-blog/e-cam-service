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

	// Asset 获取资产适配器
	Asset() AssetAdapter

	// IAM 获取IAM适配器
	IAM() IAMAdapter

	// ValidateCredentials 验证凭证
	ValidateCredentials(ctx context.Context) error
}

// AssetAdapter 资产适配器接口
type AssetAdapter interface {
	// GetRegions 获取支持的地域列表
	GetRegions(ctx context.Context) ([]types.Region, error)

	// GetECSInstances 获取云主机实例列表
	GetECSInstances(ctx context.Context, region string) ([]types.ECSInstance, error)

	// TODO: 未来扩展
	// GetRDSInstances(ctx context.Context, region string) ([]types.RDSInstance, error)
	// GetOSSBuckets(ctx context.Context, region string) ([]types.OSSBucket, error)
}

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
