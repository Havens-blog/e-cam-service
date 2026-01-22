package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// InstanceRelationRepository 实例关系仓储接口
type InstanceRelationRepository interface {
	Create(ctx context.Context, relation domain.InstanceRelation) (int64, error)
	CreateBatch(ctx context.Context, relations []domain.InstanceRelation) (int64, error)
	GetByID(ctx context.Context, id int64) (domain.InstanceRelation, error)
	List(ctx context.Context, filter domain.InstanceRelationFilter) ([]domain.InstanceRelation, error)
	Count(ctx context.Context, filter domain.InstanceRelationFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByInstanceID(ctx context.Context, instanceID int64) error
	Exists(ctx context.Context, sourceID, targetID int64, relationTypeUID string) (bool, error)
}

type instanceRelationRepository struct {
	dao dao.InstanceRelationDAO
}

// NewInstanceRelationRepository 创建实例关系仓储
func NewInstanceRelationRepository(dao dao.InstanceRelationDAO) InstanceRelationRepository {
	return &instanceRelationRepository{dao: dao}
}

// Create 创建关系
func (r *instanceRelationRepository) Create(ctx context.Context, relation domain.InstanceRelation) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(relation))
}

// CreateBatch 批量创建关系
func (r *instanceRelationRepository) CreateBatch(ctx context.Context, relations []domain.InstanceRelation) (int64, error) {
	daoRelations := make([]dao.InstanceRelation, len(relations))
	for i, rel := range relations {
		daoRelations[i] = r.toDAO(rel)
	}
	return r.dao.CreateBatch(ctx, daoRelations)
}

// GetByID 根据ID获取关系
func (r *instanceRelationRepository) GetByID(ctx context.Context, id int64) (domain.InstanceRelation, error) {
	daoRelation, err := r.dao.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.InstanceRelation{}, nil
		}
		return domain.InstanceRelation{}, err
	}
	return r.toDomain(daoRelation), nil
}

// List 获取关系列表
func (r *instanceRelationRepository) List(ctx context.Context, filter domain.InstanceRelationFilter) ([]domain.InstanceRelation, error) {
	daoRelations, err := r.dao.List(ctx, r.toDAOFilter(filter))
	if err != nil {
		return nil, err
	}

	relations := make([]domain.InstanceRelation, len(daoRelations))
	for i, daoRel := range daoRelations {
		relations[i] = r.toDomain(daoRel)
	}
	return relations, nil
}

// Count 统计关系数量
func (r *instanceRelationRepository) Count(ctx context.Context, filter domain.InstanceRelationFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

// Delete 删除关系
func (r *instanceRelationRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

// DeleteByInstanceID 删除与指定实例相关的所有关系
func (r *instanceRelationRepository) DeleteByInstanceID(ctx context.Context, instanceID int64) error {
	return r.dao.DeleteByInstanceID(ctx, instanceID)
}

// Exists 检查关系是否存在
func (r *instanceRelationRepository) Exists(ctx context.Context, sourceID, targetID int64, relationTypeUID string) (bool, error) {
	return r.dao.Exists(ctx, sourceID, targetID, relationTypeUID)
}

// toDAO 领域模型转DAO模型
func (r *instanceRelationRepository) toDAO(relation domain.InstanceRelation) dao.InstanceRelation {
	return dao.InstanceRelation{
		ID:               relation.ID,
		SourceInstanceID: relation.SourceInstanceID,
		TargetInstanceID: relation.TargetInstanceID,
		RelationTypeUID:  relation.RelationTypeUID,
		TenantID:         relation.TenantID,
	}
}

// toDomain DAO模型转领域模型
func (r *instanceRelationRepository) toDomain(daoRelation dao.InstanceRelation) domain.InstanceRelation {
	return domain.InstanceRelation{
		ID:               daoRelation.ID,
		SourceInstanceID: daoRelation.SourceInstanceID,
		TargetInstanceID: daoRelation.TargetInstanceID,
		RelationTypeUID:  daoRelation.RelationTypeUID,
		TenantID:         daoRelation.TenantID,
		CreateTime:       time.UnixMilli(daoRelation.Ctime),
	}
}

// toDAOFilter 领域过滤条件转DAO过滤条件
func (r *instanceRelationRepository) toDAOFilter(filter domain.InstanceRelationFilter) dao.InstanceRelationFilter {
	return dao.InstanceRelationFilter{
		SourceInstanceID: filter.SourceInstanceID,
		TargetInstanceID: filter.TargetInstanceID,
		RelationTypeUID:  filter.RelationTypeUID,
		TenantID:         filter.TenantID,
		Offset:           filter.Offset,
		Limit:            filter.Limit,
	}
}
