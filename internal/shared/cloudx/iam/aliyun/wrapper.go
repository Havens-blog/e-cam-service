package aliyun

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// AdapterWrapper 包装 Adapter 以实例�?CloudIAMAdapter 接口
// 这个包装器负责类型转换器，避免循环导入
type AdapterWrapper struct {
	adapter *Adapter
}

// NewAdapterWrapper 创建适配器包装器
func NewAdapterWrapper(adapter *Adapter) *AdapterWrapper {
	return &AdapterWrapper{adapter: adapter}
}

// ValidateCredentials 验证凭证
func (w *AdapterWrapper) ValidateCredentials(ctx context.Context, account *domain.CloudAccount) error {
	return w.adapter.ValidateCredentials(ctx, account)
}

// ListUsers 获取用户组列表
func (w *AdapterWrapper) ListUsers(ctx context.Context, account *domain.CloudAccount) ([]*domain.CloudUser, error) {
	return w.adapter.ListUsers(ctx, account)
}

// GetUser 获取用户组详情
func (w *AdapterWrapper) GetUser(ctx context.Context, account *domain.CloudAccount, userID string) (*domain.CloudUser, error) {
	return w.adapter.GetUser(ctx, account, userID)
}

// CreateUser 创建用户组（实例现接口方法）
func (w *AdapterWrapper) CreateUser(ctx context.Context, account *domain.CloudAccount, req *types.CreateUserRequest) (*domain.CloudUser, error) {
	// 转换器请求类型
	params := &CreateUserParams{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       req.Email,
	}
	return w.adapter.CreateUser(ctx, account, params)
}

// UpdateUserPermissions 更新用户组权限
func (w *AdapterWrapper) UpdateUserPermissions(ctx context.Context, account *domain.CloudAccount, userID string, policies []domain.PermissionPolicy) error {
	return w.adapter.UpdateUserPermissions(ctx, account, userID, policies)
}

// DeleteUser 删除用户组
func (w *AdapterWrapper) DeleteUser(ctx context.Context, account *domain.CloudAccount, userID string) error {
	return w.adapter.DeleteUser(ctx, account, userID)
}

// ListPolicies 获取权限策略列表
func (w *AdapterWrapper) ListPolicies(ctx context.Context, account *domain.CloudAccount) ([]domain.PermissionPolicy, error) {
	return w.adapter.ListPolicies(ctx, account)
}

// ListGroups 获取用户组组列�?
func (w *AdapterWrapper) ListGroups(ctx context.Context, account *domain.CloudAccount) ([]*domain.UserGroup, error) {
	return w.adapter.ListGroups(ctx, account)
}

// GetGroup 获取用户组组详�?
func (w *AdapterWrapper) GetGroup(ctx context.Context, account *domain.CloudAccount, groupID string) (*domain.UserGroup, error) {
	return w.adapter.GetGroup(ctx, account, groupID)
}

// CreateGroup 创建用户组�?
func (w *AdapterWrapper) CreateGroup(ctx context.Context, account *domain.CloudAccount, req *types.CreateGroupRequest) (*domain.UserGroup, error) {
	return w.adapter.CreateGroup(ctx, account, req)
}

// UpdateGroupPolicies 更新用户组组权限策�?
func (w *AdapterWrapper) UpdateGroupPolicies(ctx context.Context, account *domain.CloudAccount, groupID string, policies []domain.PermissionPolicy) error {
	return w.adapter.UpdateGroupPolicies(ctx, account, groupID, policies)
}

// DeleteGroup 删除用户组�?
func (w *AdapterWrapper) DeleteGroup(ctx context.Context, account *domain.CloudAccount, groupID string) error {
	return w.adapter.DeleteGroup(ctx, account, groupID)
}

// ListGroupUsers 获取用户组组成功员列�?
func (w *AdapterWrapper) ListGroupUsers(ctx context.Context, account *domain.CloudAccount, groupID string) ([]*domain.CloudUser, error) {
	return w.adapter.ListGroupUsers(ctx, account, groupID)
}

// AddUserToGroup 将用户组添加到用户组�?
func (w *AdapterWrapper) AddUserToGroup(ctx context.Context, account *domain.CloudAccount, groupID string, userID string) error {
	return w.adapter.AddUserToGroup(ctx, account, groupID, userID)
}

// RemoveUserFromGroup 将用户组从用户组组移�?
func (w *AdapterWrapper) RemoveUserFromGroup(ctx context.Context, account *domain.CloudAccount, groupID string, userID string) error {
	return w.adapter.RemoveUserFromGroup(ctx, account, groupID, userID)
}

// GetPolicy 获取策略详情
func (w *AdapterWrapper) GetPolicy(ctx context.Context, account *domain.CloudAccount, policyID string) (*domain.PermissionPolicy, error) {
	return w.adapter.GetPolicy(ctx, account, policyID)
}

// GetUserPolicies 获取用户的个人权限策略
func (w *AdapterWrapper) GetUserPolicies(ctx context.Context, account *domain.CloudAccount, userID string) ([]domain.PermissionPolicy, error) {
	return w.adapter.GetUserPolicies(ctx, account, userID)
}
