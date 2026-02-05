package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	iamrepo "github.com/Havens-blog/e-cam-service/internal/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// PermissionService 权限服务接口
type PermissionService interface {
	// GetUserPermissions 获取用户的所有权限
	GetUserPermissions(ctx context.Context, userID int64) (*UserPermissions, error)

	// GetUserGroupPermissions 获取用户组的所有权限
	GetUserGroupPermissions(ctx context.Context, groupID int64) (*GroupPermissions, error)

	// GetUserEffectivePermissions 获取用户的有效权限（包含用户组权限）
	GetUserEffectivePermissions(ctx context.Context, userID int64) (*EffectivePermissions, error)

	// ListPoliciesByProvider 按云厂商查询可用的权限策略
	ListPoliciesByProvider(ctx context.Context, cloudAccountID int64) ([]domain.PermissionPolicy, error)
}

// UserPermissions 用户权限信息
type UserPermissions struct {
	UserID         int64                     `json:"user_id"`
	Username       string                    `json:"username"`
	DisplayName    string                    `json:"display_name"`
	Provider       domain.CloudProvider      `json:"provider"`
	DirectPolicies []domain.PermissionPolicy `json:"direct_policies"` // 直接分配的权限
	UserGroups     []UserGroupInfo           `json:"user_groups"`     // 所属用户组
}

// GroupPermissions 用户组权限信息
type GroupPermissions struct {
	GroupID     int64                     `json:"group_id"`
	GroupName   string                    `json:"group_name"`
	DisplayName string                    `json:"display_name"`
	Provider    domain.CloudProvider      `json:"provider"`
	Policies    []domain.PermissionPolicy `json:"policies"`
	MemberCount int                       `json:"member_count"`
}

// EffectivePermissions 用户有效权限（合并后）
type EffectivePermissions struct {
	UserID            int64                     `json:"user_id"`
	Username          string                    `json:"username"`
	DisplayName       string                    `json:"display_name"`
	Provider          domain.CloudProvider      `json:"provider"`
	AllPolicies       []domain.PermissionPolicy `json:"all_policies"`       // 所有权限（去重）
	DirectPolicies    []domain.PermissionPolicy `json:"direct_policies"`    // 直接权限
	InheritedPolicies []domain.PermissionPolicy `json:"inherited_policies"` // 继承的权限
	UserGroups        []UserGroupInfo           `json:"user_groups"`        // 所属用户组
}

// UserGroupInfo 用户组信息
type UserGroupInfo struct {
	GroupID     int64                     `json:"group_id"`
	GroupName   string                    `json:"group_name"`
	DisplayName string                    `json:"display_name"`
	Policies    []domain.PermissionPolicy `json:"policies"`
}

type permissionService struct {
	userRepo       iamrepo.CloudUserRepository
	groupRepo      iamrepo.UserGroupRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory iam.CloudIAMAdapterFactory
	logger         *elog.Component
}

// NewPermissionService 创建权限服务
func NewPermissionService(
	userRepo iamrepo.CloudUserRepository,
	groupRepo iamrepo.UserGroupRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory iam.CloudIAMAdapterFactory,
	logger *elog.Component,
) PermissionService {
	return &permissionService{
		userRepo:       userRepo,
		groupRepo:      groupRepo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

// GetUserPermissions 获取用户的所有权限
func (s *permissionService) GetUserPermissions(ctx context.Context, userID int64) (*UserPermissions, error) {
	s.logger.Info("获取用户权限", elog.Int64("user_id", userID))

	// 获取用户信息
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		s.logger.Error("获取用户失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	result := &UserPermissions{
		UserID:         user.ID,
		Username:       user.Username,
		DisplayName:    user.DisplayName,
		Provider:       user.Provider,
		DirectPolicies: []domain.PermissionPolicy{},
		UserGroups:     []UserGroupInfo{},
	}

	// 获取用户所属的用户组及其权限
	for _, groupID := range user.UserGroups {
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			s.logger.Warn("获取用户组失败", elog.Int64("group_id", groupID), elog.FieldErr(err))
			continue
		}

		result.UserGroups = append(result.UserGroups, UserGroupInfo{
			GroupID:     group.ID,
			GroupName:   group.GroupName,
			DisplayName: group.DisplayName,
			Policies:    group.Policies,
		})
	}

	// TODO: 如果需要从云平台获取用户的直接权限，可以在这里调用适配器
	// 目前权限主要通过用户组管理

	return result, nil
}

// GetUserGroupPermissions 获取用户组的所有权限
func (s *permissionService) GetUserGroupPermissions(ctx context.Context, groupID int64) (*GroupPermissions, error) {
	s.logger.Info("获取用户组权限", elog.Int64("group_id", groupID))

	// 获取用户组信息
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		s.logger.Error("获取用户组失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取用户组失败: %w", err)
	}

	result := &GroupPermissions{
		GroupID:     group.ID,
		GroupName:   group.GroupName,
		DisplayName: group.DisplayName,
		Provider:    group.Provider,
		Policies:    group.Policies,
		MemberCount: group.MemberCount,
	}

	return result, nil
}

// GetUserEffectivePermissions 获取用户的有效权限（包含用户组权限）
func (s *permissionService) GetUserEffectivePermissions(ctx context.Context, userID int64) (*EffectivePermissions, error) {
	s.logger.Info("获取用户有效权限", elog.Int64("user_id", userID))

	// 获取用户信息
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		s.logger.Error("获取用户失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	result := &EffectivePermissions{
		UserID:            user.ID,
		Username:          user.Username,
		DisplayName:       user.DisplayName,
		Provider:          user.Provider,
		AllPolicies:       []domain.PermissionPolicy{},
		DirectPolicies:    []domain.PermissionPolicy{},
		InheritedPolicies: []domain.PermissionPolicy{},
		UserGroups:        []UserGroupInfo{},
	}

	// 用于去重的 map
	policyMap := make(map[string]domain.PermissionPolicy)

	// 获取用户所属的用户组及其权限
	for _, groupID := range user.UserGroups {
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			s.logger.Warn("获取用户组失败", elog.Int64("group_id", groupID), elog.FieldErr(err))
			continue
		}

		groupInfo := UserGroupInfo{
			GroupID:     group.ID,
			GroupName:   group.GroupName,
			DisplayName: group.DisplayName,
			Policies:    group.Policies,
		}
		result.UserGroups = append(result.UserGroups, groupInfo)

		// 收集继承的权限
		for _, policy := range group.Policies {
			key := fmt.Sprintf("%s:%s", policy.Provider, policy.PolicyID)
			if _, exists := policyMap[key]; !exists {
				policyMap[key] = policy
				result.InheritedPolicies = append(result.InheritedPolicies, policy)
			}
		}
	}

	// 合并所有权限
	result.AllPolicies = append(result.AllPolicies, result.DirectPolicies...)
	result.AllPolicies = append(result.AllPolicies, result.InheritedPolicies...)

	s.logger.Info("获取用户有效权限成功",
		elog.Int64("user_id", userID),
		elog.Int("total_policies", len(result.AllPolicies)),
		elog.Int("direct_policies", len(result.DirectPolicies)),
		elog.Int("inherited_policies", len(result.InheritedPolicies)))

	return result, nil
}

// ListPoliciesByProvider 按云厂商查询可用的权限策略
func (s *permissionService) ListPoliciesByProvider(ctx context.Context, cloudAccountID int64) ([]domain.PermissionPolicy, error) {
	s.logger.Info("查询云平台权限策略", elog.Int64("cloud_account_id", cloudAccountID))

	// 获取云账号信息
	account, err := s.accountRepo.GetByID(ctx, cloudAccountID)
	if err != nil {
		s.logger.Error("获取云账号失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	// 获取云平台适配器
	adapter, err := s.adapterFactory.CreateAdapter(account.Provider)
	if err != nil {
		s.logger.Error("获取云平台适配器失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云平台适配器失败: %w", err)
	}

	// 从云平台获取权限策略列表
	policies, err := adapter.ListPolicies(ctx, &account)
	if err != nil {
		s.logger.Error("从云平台获取权限策略失败", elog.FieldErr(err))
		return nil, fmt.Errorf("从云平台获取权限策略失败: %w", err)
	}

	s.logger.Info("查询云平台权限策略成功",
		elog.Int64("cloud_account_id", cloudAccountID),
		elog.Int("policy_count", len(policies)))

	return policies, nil
}
