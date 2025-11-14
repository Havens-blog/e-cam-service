package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// CloudUserRepository 云用户仓储接口
type CloudUserRepository interface {
	// Create 创建云用户
	Create(ctx context.Context, user domain.CloudUser) (int64, error)

	// GetByID 根据ID获取云用户
	GetByID(ctx context.Context, id int64) (domain.CloudUser, error)

	// GetByCloudUserID 根据云平台用户ID和云厂商获取用户
	GetByCloudUserID(ctx context.Context, cloudUserID string, provider domain.CloudProvider) (domain.CloudUser, error)

	// List 获取云用户列表
	List(ctx context.Context, filter domain.CloudUserFilter) ([]domain.CloudUser, int64, error)

	// Update 更新云用户
	Update(ctx context.Context, user domain.CloudUser) error

	// Delete 删除云用户
	Delete(ctx context.Context, id int64) error

	// UpdateStatus 更新用户状态
	UpdateStatus(ctx context.Context, id int64, status domain.CloudUserStatus) error

	// UpdatePermissionGroups 更新用户权限组
	UpdatePermissionGroups(ctx context.Context, id int64, groupIDs []int64) error

	// BatchUpdatePermissionGroups 批量更新用户权限组
	BatchUpdatePermissionGroups(ctx context.Context, userIDs []int64, groupIDs []int64) error

	// UpdateMetadata 更新用户元数据
	UpdateMetadata(ctx context.Context, id int64, metadata domain.CloudUserMetadata) error
}

type cloudUserRepository struct {
	dao dao.CloudUserDAO
}

// NewCloudUserRepository 创建云用户仓储
func NewCloudUserRepository(dao dao.CloudUserDAO) CloudUserRepository {
	return &cloudUserRepository{
		dao: dao,
	}
}

// Create 创建云用户
func (repo *cloudUserRepository) Create(ctx context.Context, user domain.CloudUser) (int64, error) {
	daoUser := repo.toEntity(user)
	return repo.dao.Create(ctx, daoUser)
}

// GetByID 根据ID获取云用户
func (repo *cloudUserRepository) GetByID(ctx context.Context, id int64) (domain.CloudUser, error) {
	daoUser, err := repo.dao.GetByID(ctx, id)
	if err != nil {
		return domain.CloudUser{}, err
	}
	return repo.toDomain(daoUser), nil
}

// GetByCloudUserID 根据云平台用户ID和云厂商获取用户
func (repo *cloudUserRepository) GetByCloudUserID(ctx context.Context, cloudUserID string, provider domain.CloudProvider) (domain.CloudUser, error) {
	daoUser, err := repo.dao.GetByCloudUserID(ctx, cloudUserID, dao.CloudProvider(provider))
	if err != nil {
		return domain.CloudUser{}, err
	}
	return repo.toDomain(daoUser), nil
}

// List 获取云用户列表
func (repo *cloudUserRepository) List(ctx context.Context, filter domain.CloudUserFilter) ([]domain.CloudUser, int64, error) {
	daoFilter := dao.CloudUserFilter{
		Provider:       dao.CloudProvider(filter.Provider),
		UserType:       dao.CloudUserType(filter.UserType),
		Status:         dao.CloudUserStatus(filter.Status),
		CloudAccountID: filter.CloudAccountID,
		TenantID:       filter.TenantID,
		Keyword:        filter.Keyword,
		Offset:         filter.Offset,
		Limit:          filter.Limit,
	}

	daoUsers, err := repo.dao.List(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.Count(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	users := make([]domain.CloudUser, len(daoUsers))
	for i, daoUser := range daoUsers {
		users[i] = repo.toDomain(daoUser)
	}

	return users, count, nil
}

// Update 更新云用户
func (repo *cloudUserRepository) Update(ctx context.Context, user domain.CloudUser) error {
	daoUser := repo.toEntity(user)
	return repo.dao.Update(ctx, daoUser)
}

// Delete 删除云用户
func (repo *cloudUserRepository) Delete(ctx context.Context, id int64) error {
	return repo.dao.Delete(ctx, id)
}

// UpdateStatus 更新用户状态
func (repo *cloudUserRepository) UpdateStatus(ctx context.Context, id int64, status domain.CloudUserStatus) error {
	return repo.dao.UpdateStatus(ctx, id, dao.CloudUserStatus(status))
}

// UpdatePermissionGroups 更新用户权限组
func (repo *cloudUserRepository) UpdatePermissionGroups(ctx context.Context, id int64, groupIDs []int64) error {
	return repo.dao.UpdatePermissionGroups(ctx, id, groupIDs)
}

// BatchUpdatePermissionGroups 批量更新用户权限组
func (repo *cloudUserRepository) BatchUpdatePermissionGroups(ctx context.Context, userIDs []int64, groupIDs []int64) error {
	return repo.dao.BatchUpdatePermissionGroups(ctx, userIDs, groupIDs)
}

// UpdateMetadata 更新用户元数据
func (repo *cloudUserRepository) UpdateMetadata(ctx context.Context, id int64, metadata domain.CloudUserMetadata) error {
	daoMetadata := dao.CloudUserMetadata{
		LastLoginTime:   metadata.LastLoginTime,
		LastSyncTime:    metadata.LastSyncTime,
		AccessKeyCount:  metadata.AccessKeyCount,
		MFAEnabled:      metadata.MFAEnabled,
		PasswordLastSet: metadata.PasswordLastSet,
		Tags:            metadata.Tags,
	}
	return repo.dao.UpdateMetadata(ctx, id, daoMetadata)
}

// toDomain 转换为领域模型
func (repo *cloudUserRepository) toDomain(daoUser dao.CloudUser) domain.CloudUser {
	return domain.CloudUser{
		ID:             daoUser.ID,
		Username:       daoUser.Username,
		UserType:       domain.CloudUserType(daoUser.UserType),
		CloudAccountID: daoUser.CloudAccountID,
		Provider:       domain.CloudProvider(daoUser.Provider),
		CloudUserID:    daoUser.CloudUserID,
		DisplayName:    daoUser.DisplayName,
		Email:          daoUser.Email,
		PermissionGroups: daoUser.PermissionGroups,
		Metadata: domain.CloudUserMetadata{
			LastLoginTime:   daoUser.Metadata.LastLoginTime,
			LastSyncTime:    daoUser.Metadata.LastSyncTime,
			AccessKeyCount:  daoUser.Metadata.AccessKeyCount,
			MFAEnabled:      daoUser.Metadata.MFAEnabled,
			PasswordLastSet: daoUser.Metadata.PasswordLastSet,
			Tags:            daoUser.Metadata.Tags,
		},
		Status:     domain.CloudUserStatus(daoUser.Status),
		TenantID:   daoUser.TenantID,
		CreateTime: daoUser.CreateTime,
		UpdateTime: daoUser.UpdateTime,
		CTime:      daoUser.CTime,
		UTime:      daoUser.UTime,
	}
}

// toEntity 转换为DAO实体
func (repo *cloudUserRepository) toEntity(user domain.CloudUser) dao.CloudUser {
	return dao.CloudUser{
		ID:               user.ID,
		Username:         user.Username,
		UserType:         dao.CloudUserType(user.UserType),
		CloudAccountID:   user.CloudAccountID,
		Provider:         dao.CloudProvider(user.Provider),
		CloudUserID:      user.CloudUserID,
		DisplayName:      user.DisplayName,
		Email:            user.Email,
		PermissionGroups: user.PermissionGroups,
		Metadata: dao.CloudUserMetadata{
			LastLoginTime:   user.Metadata.LastLoginTime,
			LastSyncTime:    user.Metadata.LastSyncTime,
			AccessKeyCount:  user.Metadata.AccessKeyCount,
			MFAEnabled:      user.Metadata.MFAEnabled,
			PasswordLastSet: user.Metadata.PasswordLastSet,
			Tags:            user.Metadata.Tags,
		},
		Status:     dao.CloudUserStatus(user.Status),
		TenantID:   user.TenantID,
		CreateTime: user.CreateTime,
		UpdateTime: user.UpdateTime,
		CTime:      user.CTime,
		UTime:      user.UTime,
	}
}
