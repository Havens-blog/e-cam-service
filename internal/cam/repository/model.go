package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
)

// ModelRepository 模型仓储接口
type ModelRepository interface {
	// CreateModel 创建模型
	CreateModel(ctx context.Context, model domain.Model) (int64, error)

	// GetModelByUID 根据UID获取模型
	GetModelByUID(ctx context.Context, uid string) (domain.Model, error)

	// GetModelByID 根据ID获取模型
	GetModelByID(ctx context.Context, id int64) (domain.Model, error)

	// ListModels 获取模型列表
	ListModels(ctx context.Context, filter domain.ModelFilter) ([]domain.Model, error)

	// CountModels 统计模型数量
	CountModels(ctx context.Context, filter domain.ModelFilter) (int64, error)

	// UpdateModel 更新模型
	UpdateModel(ctx context.Context, model domain.Model) error

	// DeleteModel 删除模型
	DeleteModel(ctx context.Context, uid string) error

	// ModelExists 检查模型是否存在
	ModelExists(ctx context.Context, uid string) (bool, error)
}

type modelRepository struct {
	dao dao.ModelDAO
}

// NewModelRepository 创建模型仓储
func NewModelRepository(dao dao.ModelDAO) ModelRepository {
	return &modelRepository{
		dao: dao,
	}
}

// CreateModel 创建模型
func (r *modelRepository) CreateModel(ctx context.Context, model domain.Model) (int64, error) {
	daoModel := r.toEntity(model)
	return r.dao.CreateModel(ctx, daoModel)
}

// GetModelByUID 根据UID获取模型
func (r *modelRepository) GetModelByUID(ctx context.Context, uid string) (domain.Model, error) {
	daoModel, err := r.dao.GetModelByUID(ctx, uid)
	if err != nil {
		return domain.Model{}, err
	}
	return r.toDomain(daoModel), nil
}

// GetModelByID 根据ID获取模型
func (r *modelRepository) GetModelByID(ctx context.Context, id int64) (domain.Model, error) {
	daoModel, err := r.dao.GetModelByID(ctx, id)
	if err != nil {
		return domain.Model{}, err
	}
	return r.toDomain(daoModel), nil
}

// ListModels 获取模型列表
func (r *modelRepository) ListModels(ctx context.Context, filter domain.ModelFilter) ([]domain.Model, error) {
	daoFilter := dao.ModelFilter{
		Provider:   filter.Provider,
		Category:   filter.Category,
		ParentUID:  filter.ParentUID,
		Level:      filter.Level,
		Extensible: filter.Extensible,
		Offset:     filter.Offset,
		Limit:      filter.Limit,
	}

	daoModels, err := r.dao.ListModels(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	models := make([]domain.Model, len(daoModels))
	for i, daoModel := range daoModels {
		models[i] = r.toDomain(daoModel)
	}

	return models, nil
}

// CountModels 统计模型数量
func (r *modelRepository) CountModels(ctx context.Context, filter domain.ModelFilter) (int64, error) {
	daoFilter := dao.ModelFilter{
		Provider:   filter.Provider,
		Category:   filter.Category,
		ParentUID:  filter.ParentUID,
		Level:      filter.Level,
		Extensible: filter.Extensible,
	}

	return r.dao.CountModels(ctx, daoFilter)
}

// UpdateModel 更新模型
func (r *modelRepository) UpdateModel(ctx context.Context, model domain.Model) error {
	daoModel := r.toEntity(model)
	return r.dao.UpdateModel(ctx, daoModel)
}

// DeleteModel 删除模型
func (r *modelRepository) DeleteModel(ctx context.Context, uid string) error {
	return r.dao.DeleteModel(ctx, uid)
}

// ModelExists 检查模型是否存在
func (r *modelRepository) ModelExists(ctx context.Context, uid string) (bool, error) {
	return r.dao.ModelExists(ctx, uid)
}

// toDomain 将DAO对象转换为领域对象
func (r *modelRepository) toDomain(daoModel dao.Model) domain.Model {
	return domain.Model{
		ID:           daoModel.ID,
		UID:          daoModel.UID,
		Name:         daoModel.Name,
		ModelGroupID: daoModel.ModelGroupID,
		ParentUID:    daoModel.ParentUID,
		Category:     daoModel.Category,
		Level:        daoModel.Level,
		Icon:         daoModel.Icon,
		Description:  daoModel.Description,
		Provider:     daoModel.Provider,
		Extensible:   daoModel.Extensible,
		CreateTime:   time.UnixMilli(daoModel.Ctime),
		UpdateTime:   time.UnixMilli(daoModel.Utime),
	}
}

// toEntity 将领域对象转换为DAO对象
func (r *modelRepository) toEntity(model domain.Model) dao.Model {
	return dao.Model{
		ID:           model.ID,
		UID:          model.UID,
		Name:         model.Name,
		ModelGroupID: model.ModelGroupID,
		ParentUID:    model.ParentUID,
		Category:     model.Category,
		Level:        model.Level,
		Icon:         model.Icon,
		Description:  model.Description,
		Provider:     model.Provider,
		Extensible:   model.Extensible,
		Ctime:        model.CreateTime.UnixMilli(),
		Utime:        model.UpdateTime.UnixMilli(),
	}
}
