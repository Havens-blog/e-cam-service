// Package repository 资产仓储层
package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/asset/domain"
	"github.com/Havens-blog/e-cam-service/internal/asset/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// InstanceRepository 资产实例仓储接口
type InstanceRepository interface {
	Create(ctx context.Context, instance domain.Instance) (int64, error)
	CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error)
	Update(ctx context.Context, instance domain.Instance) error
	GetByID(ctx context.Context, id int64) (domain.Instance, error)
	GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (domain.Instance, error)
	List(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, error)
	Count(ctx context.Context, filter domain.InstanceFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByAccountID(ctx context.Context, accountID int64) error
	DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error)
	ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error)
	ListAssetIDsByModelUID(ctx context.Context, tenantID, modelUID string, accountID int64) ([]string, error)
	Upsert(ctx context.Context, instance domain.Instance) error
	Search(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error)
}

type instanceRepository struct {
	dao dao.InstanceDAO
}

func NewInstanceRepository(dao dao.InstanceDAO) InstanceRepository {
	return &instanceRepository{dao: dao}
}

func (r *instanceRepository) Create(ctx context.Context, instance domain.Instance) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(instance))
}

func (r *instanceRepository) CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error) {
	daoInstances := make([]dao.Instance, len(instances))
	for i, inst := range instances {
		daoInstances[i] = r.toDAO(inst)
	}
	return r.dao.CreateBatch(ctx, daoInstances)
}

func (r *instanceRepository) Update(ctx context.Context, instance domain.Instance) error {
	return r.dao.Update(ctx, r.toDAO(instance))
}

func (r *instanceRepository) GetByID(ctx context.Context, id int64) (domain.Instance, error) {
	daoInstance, err := r.dao.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.Instance{}, nil
		}
		return domain.Instance{}, err
	}
	return r.toDomain(daoInstance), nil
}

func (r *instanceRepository) GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (domain.Instance, error) {
	daoInstance, err := r.dao.GetByAssetID(ctx, tenantID, modelUID, assetID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.Instance{}, nil
		}
		return domain.Instance{}, err
	}
	return r.toDomain(daoInstance), nil
}

func (r *instanceRepository) List(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, error) {
	daoInstances, err := r.dao.List(ctx, r.toDAOFilter(filter))
	if err != nil {
		return nil, err
	}
	instances := make([]domain.Instance, len(daoInstances))
	for i, daoInst := range daoInstances {
		instances[i] = r.toDomain(daoInst)
	}
	return instances, nil
}

func (r *instanceRepository) Count(ctx context.Context, filter domain.InstanceFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

func (r *instanceRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

func (r *instanceRepository) DeleteByAccountID(ctx context.Context, accountID int64) error {
	return r.dao.DeleteByAccountID(ctx, accountID)
}

func (r *instanceRepository) DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error) {
	return r.dao.DeleteByAssetIDs(ctx, tenantID, modelUID, assetIDs)
}

func (r *instanceRepository) ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error) {
	return r.dao.ListAssetIDsByRegion(ctx, tenantID, modelUID, accountID, region)
}

func (r *instanceRepository) ListAssetIDsByModelUID(ctx context.Context, tenantID, modelUID string, accountID int64) ([]string, error) {
	return r.dao.ListAssetIDsByModelUID(ctx, tenantID, modelUID, accountID)
}

func (r *instanceRepository) Upsert(ctx context.Context, instance domain.Instance) error {
	return r.dao.Upsert(ctx, r.toDAO(instance))
}

func (r *instanceRepository) Search(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error) {
	daoFilter := dao.SearchFilter{
		TenantID:   filter.TenantID,
		Keyword:    filter.Keyword,
		AssetTypes: filter.AssetTypes,
		Provider:   filter.Provider,
		AccountID:  filter.AccountID,
		Region:     filter.Region,
		Offset:     filter.Offset,
		Limit:      filter.Limit,
	}
	daoInstances, total, err := r.dao.Search(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}
	instances := make([]domain.Instance, len(daoInstances))
	for i, daoInst := range daoInstances {
		instances[i] = r.toDomain(daoInst)
	}
	return instances, total, nil
}

func (r *instanceRepository) toDAO(instance domain.Instance) dao.Instance {
	return dao.Instance{
		ID:         instance.ID,
		ModelUID:   instance.ModelUID,
		AssetID:    instance.AssetID,
		AssetName:  instance.AssetName,
		TenantID:   instance.TenantID,
		AccountID:  instance.AccountID,
		Attributes: instance.Attributes,
	}
}

func (r *instanceRepository) toDomain(daoInstance dao.Instance) domain.Instance {
	return domain.Instance{
		ID:         daoInstance.ID,
		ModelUID:   daoInstance.ModelUID,
		AssetID:    daoInstance.AssetID,
		AssetName:  daoInstance.AssetName,
		TenantID:   daoInstance.TenantID,
		AccountID:  daoInstance.AccountID,
		Attributes: daoInstance.Attributes,
		CreateTime: time.UnixMilli(daoInstance.Ctime),
		UpdateTime: time.UnixMilli(daoInstance.Utime),
	}
}

func (r *instanceRepository) toDAOFilter(filter domain.InstanceFilter) dao.InstanceFilter {
	daoFilter := dao.InstanceFilter{
		ModelUID:   filter.ModelUID,
		TenantID:   filter.TenantID,
		AccountID:  filter.AccountID,
		AssetID:    filter.AssetID,
		AssetName:  filter.AssetName,
		Provider:   filter.Provider,
		Attributes: filter.Attributes,
		Offset:     filter.Offset,
		Limit:      filter.Limit,
	}
	if filter.TagFilter != nil {
		daoFilter.TagFilter = &dao.TagFilter{
			HasTags: filter.TagFilter.HasTags,
			NoTags:  filter.TagFilter.NoTags,
			Key:     filter.TagFilter.Key,
			Value:   filter.TagFilter.Value,
		}
	}
	return daoFilter
}
