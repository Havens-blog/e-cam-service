package iam

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// CloudIAMAdapter 云平台IAM适配器接口
type CloudIAMAdapter interface {
	// ListUsers 获取用户列表
	ListUsers(ctx context.Context, account *domain.CloudAccount) ([]*domain.CloudUser, error)

	// GetUser 获取用户详情
	GetUser(ctx context.Context, account *domain.CloudAccount, userID string) (*domain.CloudUser, error)

	// CreateUser 创建用户
	CreateUser(ctx context.Context, account *domain.CloudAccount, req *types.CreateUserRequest) (*domain.CloudUser, error)

	// UpdateUserPermissions 更新用户权限
	UpdateUserPermissions(ctx context.Context, account *domain.CloudAccount, userID string, policies []domain.PermissionPolicy) error

	// DeleteUser 删除用户
	DeleteUser(ctx context.Context, account *domain.CloudAccount, userID string) error

	// ListPolicies 获取权限策略列表
	ListPolicies(ctx context.Context, account *domain.CloudAccount) ([]domain.PermissionPolicy, error)

	// ValidateCredentials 验证凭证
	ValidateCredentials(ctx context.Context, account *domain.CloudAccount) error
}

// CloudIAMAdapterFactory 云平台IAM适配器工厂接口
type CloudIAMAdapterFactory interface {
	// CreateAdapter 根据云厂商类型创建适配器
	CreateAdapter(provider domain.CloudProvider) (CloudIAMAdapter, error)
}
