package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
)

// ModelFieldGroupRepository 字段分组仓储接口
type ModelFieldGroupRepository interface {
	// CreateGroup 创建分组
	CreateGroup(ctx context.Context, group domain.ModelFieldGroup) (int64, error)

	// GetGroupByID 根据ID获取分组
	GetGroupByID(ctx context.Context, id int64) (domain.ModelFieldGroup, error)

	// ListGroups 获取分组列表
	ListGroups(ctx context.Context, filter domain.ModelFieldGroupFilter) ([]domain.ModelFieldGroup, error)

	// GetGroupsByModelUID 获取模型的所有分组
	GetGroupsByModelUID(ctx context.Context, modelUID string) ([]domain.ModelFieldGroup, error)

	// UpdateGroup 更新分组
	UpdateGroup(ctx context.Context, group domain.ModelFieldGroup) error

	// DeleteGroup 删除分组
	DeleteGroup(ctx context.Context, id int64) error

	// DeleteGroupsByModelUID 删除模型的所有分组
	DeleteGroupsByModelUID(ctx context.Context, modelUID string) error
}

type modelFieldGroupRepository struct {
	dao dao.ModelFieldGroupDAO
}

// NewModelFieldGroupRepository 创建字段分组仓储
func NewModelFieldGroupRepository(dao dao.ModelFieldGroupDAO) ModelFieldGroupRepository {
	return &modelFieldGroupRepository{
		dao: dao,
	}
}

// CreateGroup 创建分组
func (r *modelFieldGroupRepository) CreateGroup(ctx context.Context, group domain.ModelFieldGroup) (int64, error) {
	daoGroup := r.toEntity(group)
	return r.dao.CreateGroup(ctx, daoGroup)
}

// GetGroupByID 根据ID获取分组
func (r *modelFieldGroupRepository) GetGroupByID(ctx context.Context, id int64) (domain.ModelFieldGroup, error) {
	daoGroup, err := r.dao.GetGroupByID(ctx, id)
	if err != nil {
		return domain.ModelFieldGroup{}, err
	}
	return r.toDomain(daoGroup), nil
}

// ListGroups 获取分组列表
func (r *modelFieldGroupRepository) ListGroups(ctx context.Context, filter domain.ModelFieldGroupFilter) ([]domain.ModelFieldGroup, error) {
	daoFilter := dao.ModelFieldGroupFilter{
		ModelUID: filter.ModelUID,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}

	daoGroups, err := r.dao.ListGroups(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	groups := make([]domain.ModelFieldGroup, len(daoGroups))
	for i, daoGroup := range daoGroups {
		groups[i] = r.toDomain(daoGroup)
	}

	return groups, nil
}

// GetGroupsByModelUID 获取模型的所有分组
func (r *modelFieldGroupRepository) GetGroupsByModelUID(ctx context.Context, modelUID string) ([]domain.ModelFieldGroup, error) {
	daoGroups, err := r.dao.GetGroupsByModelUID(ctx, modelUID)
	if err != nil {
		return nil, err
	}

	groups := make([]domain.ModelFieldGroup, len(daoGroups))
	for i, daoGroup := range daoGroups {
		groups[i] = r.toDomain(daoGroup)
	}

	return groups, nil
}

// UpdateGroup 更新分组
func (r *modelFieldGroupRepository) UpdateGroup(ctx context.Context, group domain.ModelFieldGroup) error {
	daoGroup := r.toEntity(group)
	return r.dao.UpdateGroup(ctx, daoGroup)
}

// DeleteGroup 删除分组
func (r *modelFieldGroupRepository) DeleteGroup(ctx context.Context, id int64) error {
	return r.dao.DeleteGroup(ctx, id)
}

// DeleteGroupsByModelUID 删除模型的所有分组
func (r *modelFieldGroupRepository) DeleteGroupsByModelUID(ctx context.Context, modelUID string) error {
	return r.dao.DeleteGroupsByModelUID(ctx, modelUID)
}

// toDomain 将DAO对象转换为领域对象
func (r *modelFieldGroupRepository) toDomain(daoGroup dao.ModelFieldGroup) domain.ModelFieldGroup {
	return domain.ModelFieldGroup{
		ID:         daoGroup.ID,
		ModelUID:   daoGroup.ModelUID,
		Name:       daoGroup.Name,
		Index:      daoGroup.Index,
		CreateTime: time.UnixMilli(daoGroup.Ctime),
		UpdateTime: time.UnixMilli(daoGroup.Utime),
	}
}

// toEntity 将领域对象转换为DAO对象
func (r *modelFieldGroupRepository) toEntity(group domain.ModelFieldGroup) dao.ModelFieldGroup {
	return dao.ModelFieldGroup{
		ID:       group.ID,
		ModelUID: group.ModelUID,
		Name:     group.Name,
		Index:    group.Index,
		Ctime:    group.CreateTime.UnixMilli(),
		Utime:    group.UpdateTime.UnixMilli(),
	}
}
