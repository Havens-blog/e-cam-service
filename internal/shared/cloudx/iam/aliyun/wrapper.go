package aliyun

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// AdapterWrapper 包装 Adapter 以实现 CloudIAMAdapter 接口
// 这个包装器负责类型转换，避免循环导入
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

// ListUsers 获取用户列表
func (w *AdapterWrapper) ListUsers(ctx context.Context, account *domain.CloudAccount) ([]*domain.CloudUser, error) {
	return w.adapter.ListUsers(ctx, account)
}

// GetUser 获取用户详情
func (w *AdapterWrapper) GetUser(ctx context.Context, account *domain.CloudAccount, userID string) (*domain.CloudUser, error) {
	return w.adapter.GetUser(ctx, account, userID)
}

// CreateUser 创建用户（实现接口方法）
func (w *AdapterWrapper) CreateUser(ctx context.Context, account *domain.CloudAccount, req *types.CreateUserRequest) (*domain.CloudUser, error) {
	// 转换请求类型
	params := &CreateUserParams{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       req.Email,
	}
	return w.adapter.CreateUser(ctx, account, params)
}

// UpdateUserPermissions 更新用户权限
func (w *AdapterWrapper) UpdateUserPermissions(ctx context.Context, account *domain.CloudAccount, userID string, policies []domain.PermissionPolicy) error {
	return w.adapter.UpdateUserPermissions(ctx, account, userID, policies)
}

// DeleteUser 删除用户
func (w *AdapterWrapper) DeleteUser(ctx context.Context, account *domain.CloudAccount, userID string) error {
	return w.adapter.DeleteUser(ctx, account, userID)
}

// ListPolicies 获取权限策略列表
func (w *AdapterWrapper) ListPolicies(ctx context.Context, account *domain.CloudAccount) ([]domain.PermissionPolicy, error) {
	return w.adapter.ListPolicies(ctx, account)
}
