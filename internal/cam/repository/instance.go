package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
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
	Upsert(ctx context.Context, instance domain.Instance) error
}

type instanceRepository struct {
	dao dao.InstanceDAO
}

// NewInstanceRepository 创建实例仓储
func NewInstanceRepository(dao dao.InstanceDAO) InstanceRepository {
	return &instanceRepository{dao: dao}
}

// Create 创建实例
func (r *instanceRepository) Create(ctx context.Context, instance domain.Instance) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(instance))
}

// CreateBatch 批量创建实例
func (r *instanceRepository) CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error) {
	daoInstances := make([]dao.Instance, len(instances))
	for i, inst := range instances {
		daoInstances[i] = r.toDAO(inst)
	}
	return r.dao.CreateBatch(ctx, daoInstances)
}

// Update 更新实例
func (r *instanceRepository) Update(ctx context.Context, instance domain.Instance) error {
	return r.dao.Update(ctx, r.toDAO(instance))
}

// GetByID 根据ID获取实例
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

// GetByAssetID 根据云厂商资产ID获取实例
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

// List 获取实例列表
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

// Count 统计实例数量
func (r *instanceRepository) Count(ctx context.Context, filter domain.InstanceFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

// Delete 删除实例
func (r *instanceRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

// DeleteByAccountID 删除指定云账号的所有实例
func (r *instanceRepository) DeleteByAccountID(ctx context.Context, accountID int64) error {
	return r.dao.DeleteByAccountID(ctx, accountID)
}

// Upsert 更新或插入实例
func (r *instanceRepository) Upsert(ctx context.Context, instance domain.Instance) error {
	return r.dao.Upsert(ctx, r.toDAO(instance))
}

// DeleteByAssetIDs 根据 AssetID 列表批量删除实例
func (r *instanceRepository) DeleteByAssetIDs(ctx context.Context, tenantID, modelUID string, assetIDs []string) (int64, error) {
	return r.dao.DeleteByAssetIDs(ctx, tenantID, modelUID, assetIDs)
}

// ListAssetIDsByRegion 获取指定地域的所有 AssetID 列表
func (r *instanceRepository) ListAssetIDsByRegion(ctx context.Context, tenantID, modelUID string, accountID int64, region string) ([]string, error) {
	return r.dao.ListAssetIDsByRegion(ctx, tenantID, modelUID, accountID, region)
}

// toDAO 领域模型转DAO模型
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

// toDomain DAO模型转领域模型
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

// toDAOFilter 领域过滤条件转DAO过滤条件
func (r *instanceRepository) toDAOFilter(filter domain.InstanceFilter) dao.InstanceFilter {
	return dao.InstanceFilter{
		ModelUID:   filter.ModelUID,
		TenantID:   filter.TenantID,
		AccountID:  filter.AccountID,
		AssetName:  filter.AssetName,
		Attributes: filter.Attributes,
		Offset:     filter.Offset,
		Limit:      filter.Limit,
	}
}
