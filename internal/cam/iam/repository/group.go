package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// UserGroupRepository 用户组仓储接口
type UserGroupRepository interface {
	// Create 创建用户组
	Create(ctx context.Context, group domain.UserGroup) (int64, error)

	// GetByID 根据ID获取用户组
	GetByID(ctx context.Context, id int64) (domain.UserGroup, error)

	// GetByName 根据名称和租户ID获取用户组
	GetByName(ctx context.Context, name, tenantID string) (domain.UserGroup, error)

	// List 获取用户组列表
	List(ctx context.Context, filter domain.UserGroupFilter) ([]domain.UserGroup, int64, error)

	// Update 更新用户组
	Update(ctx context.Context, group domain.UserGroup) error

	// Delete 删除用户组
	Delete(ctx context.Context, id int64) error

	// UpdatePolicies 更新权限策略
	UpdatePolicies(ctx context.Context, id int64, policies []domain.PermissionPolicy) error

	// IncrementUserCount 增加或减少用户数量
	IncrementUserCount(ctx context.Context, id int64, delta int) error
}

type userGroupRepository struct {
	dao dao.UserGroupDAO
}

// NewUserGroupRepository 创建用户组仓储
func NewUserGroupRepository(dao dao.UserGroupDAO) UserGroupRepository {
	return &userGroupRepository{
		dao: dao,
	}
}

// Create 创建用户组
func (repo *userGroupRepository) Create(ctx context.Context, group domain.UserGroup) (int64, error) {
	daoGroup := repo.toEntity(group)
	return repo.dao.Create(ctx, daoGroup)
}

// GetByID 根据ID获取用户组
func (repo *userGroupRepository) GetByID(ctx context.Context, id int64) (domain.UserGroup, error) {
	daoGroup, err := repo.dao.GetByID(ctx, id)
	if err != nil {
		return domain.UserGroup{}, err
	}
	return repo.toDomain(daoGroup), nil
}

// GetByName 根据名称和租户ID获取用户组
func (repo *userGroupRepository) GetByName(ctx context.Context, name, tenantID string) (domain.UserGroup, error) {
	daoGroup, err := repo.dao.GetByName(ctx, name, tenantID)
	if err != nil {
		return domain.UserGroup{}, err
	}
	return repo.toDomain(daoGroup), nil
}

// List 获取用户组列表
func (repo *userGroupRepository) List(ctx context.Context, filter domain.UserGroupFilter) ([]domain.UserGroup, int64, error) {
	daoFilter := dao.UserGroupFilter{
		TenantID: filter.TenantID,
		Keyword:  filter.Keyword,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}

	daoGroups, err := repo.dao.List(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.Count(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	groups := make([]domain.UserGroup, len(daoGroups))
	for i, daoGroup := range daoGroups {
		groups[i] = repo.toDomain(daoGroup)
	}

	return groups, count, nil
}

// Update 更新用户组
func (repo *userGroupRepository) Update(ctx context.Context, group domain.UserGroup) error {
	daoGroup := repo.toEntity(group)
	return repo.dao.Update(ctx, daoGroup)
}

// Delete 删除用户组
func (repo *userGroupRepository) Delete(ctx context.Context, id int64) error {
	return repo.dao.Delete(ctx, id)
}

// UpdatePolicies 更新权限策略
func (repo *userGroupRepository) UpdatePolicies(ctx context.Context, id int64, policies []domain.PermissionPolicy) error {
	daoPolicies := make([]dao.PermissionPolicy, len(policies))
	for i, policy := range policies {
		daoPolicies[i] = dao.PermissionPolicy{
			PolicyID:       policy.PolicyID,
			PolicyName:     policy.PolicyName,
			PolicyDocument: policy.PolicyDocument,
			Provider:       dao.CloudProvider(policy.Provider),
			PolicyType:     dao.PolicyType(policy.PolicyType),
		}
	}
	return repo.dao.UpdatePolicies(ctx, id, daoPolicies)
}

// IncrementUserCount 增加或减少用户数量
func (repo *userGroupRepository) IncrementUserCount(ctx context.Context, id int64, delta int) error {
	return repo.dao.IncrementUserCount(ctx, id, delta)
}

// toDomain 转换为领域模型
func (repo *userGroupRepository) toDomain(daoGroup dao.UserGroup) domain.UserGroup {
	policies := make([]domain.PermissionPolicy, len(daoGroup.Policies))
	for i, policy := range daoGroup.Policies {
		policies[i] = domain.PermissionPolicy{
			PolicyID:       policy.PolicyID,
			PolicyName:     policy.PolicyName,
			PolicyDocument: policy.PolicyDocument,
			Provider:       domain.CloudProvider(policy.Provider),
			PolicyType:     domain.PolicyType(policy.PolicyType),
		}
	}

	cloudPlatforms := make([]domain.CloudProvider, len(daoGroup.CloudPlatforms))
	for i, platform := range daoGroup.CloudPlatforms {
		cloudPlatforms[i] = domain.CloudProvider(platform)
	}

	return domain.UserGroup{
		ID:             daoGroup.ID,
		Name:           daoGroup.Name,
		Description:    daoGroup.Description,
		Policies:       policies,
		CloudPlatforms: cloudPlatforms,
		UserCount:      daoGroup.UserCount,
		TenantID:       daoGroup.TenantID,
		CreateTime:     daoGroup.CreateTime,
		UpdateTime:     daoGroup.UpdateTime,
		CTime:          daoGroup.CTime,
		UTime:          daoGroup.UTime,
	}
}

// toEntity 转换为DAO实体
func (repo *userGroupRepository) toEntity(group domain.UserGroup) dao.UserGroup {
	policies := make([]dao.PermissionPolicy, len(group.Policies))
	for i, policy := range group.Policies {
		policies[i] = dao.PermissionPolicy{
			PolicyID:       policy.PolicyID,
			PolicyName:     policy.PolicyName,
			PolicyDocument: policy.PolicyDocument,
			Provider:       dao.CloudProvider(policy.Provider),
			PolicyType:     dao.PolicyType(policy.PolicyType),
		}
	}

	cloudPlatforms := make([]dao.CloudProvider, len(group.CloudPlatforms))
	for i, platform := range group.CloudPlatforms {
		cloudPlatforms[i] = dao.CloudProvider(platform)
	}

	return dao.UserGroup{
		ID:             group.ID,
		Name:           group.Name,
		Description:    group.Description,
		Policies:       policies,
		CloudPlatforms: cloudPlatforms,
		UserCount:      group.UserCount,
		TenantID:       group.TenantID,
		CreateTime:     group.CreateTime,
		UpdateTime:     group.UpdateTime,
		CTime:          group.CTime,
		UTime:          group.UTime,
	}
}
