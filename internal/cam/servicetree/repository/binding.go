package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// BindingRepository 资源绑定仓储接口
type BindingRepository interface {
	Create(ctx context.Context, binding domain.ResourceBinding) (int64, error)
	CreateBatch(ctx context.Context, bindings []domain.ResourceBinding) (int64, error)
	GetByID(ctx context.Context, id int64) (domain.ResourceBinding, error)
	GetByResource(ctx context.Context, tenantID int64, resourceType string, resourceID int64) (domain.ResourceBinding, error)
	List(ctx context.Context, filter domain.BindingFilter) ([]domain.ResourceBinding, error)
	ListByNodeIDs(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error)
	Count(ctx context.Context, filter domain.BindingFilter) (int64, error)
	CountByNodeID(ctx context.Context, nodeID int64) (int64, error)
	CountByNodeIDs(ctx context.Context, filter domain.NodeIDsBindingFilter) (int64, error)
	CountByRuleIDs(ctx context.Context, ruleIDs []int64) (map[int64]int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByNodeID(ctx context.Context, nodeID int64) error
	DeleteByResource(ctx context.Context, tenantID int64, resourceType string, resourceID int64) error
	DeleteByRuleID(ctx context.Context, ruleID int64) (int64, error)
	// UpdateTarget 改绑：仅更新归属三字段（节点/环境/来源规则），不动 bind_type 与资源标识。
	// 资源唯一索引为 tenant_id+resource_type+resource_id，故改绑必须原地更新而非新建。
	UpdateTarget(ctx context.Context, id int64, nodeID int64, envID int64, ruleID int64) error
}

type bindingRepository struct {
	dao dao.BindingDAO
}

// NewBindingRepository 创建绑定仓储
func NewBindingRepository(dao dao.BindingDAO) BindingRepository {
	return &bindingRepository{dao: dao}
}

func (r *bindingRepository) Create(ctx context.Context, binding domain.ResourceBinding) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(binding))
}

func (r *bindingRepository) CreateBatch(ctx context.Context, bindings []domain.ResourceBinding) (int64, error) {
	daoBindings := make([]dao.Binding, len(bindings))
	for i, b := range bindings {
		daoBindings[i] = r.toDAO(b)
	}
	return r.dao.CreateBatch(ctx, daoBindings)
}

func (r *bindingRepository) GetByID(ctx context.Context, id int64) (domain.ResourceBinding, error) {
	daoBinding, err := r.dao.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.ResourceBinding{}, domain.ErrBindingNotFound
		}
		return domain.ResourceBinding{}, err
	}
	return r.toDomain(daoBinding), nil
}

func (r *bindingRepository) GetByResource(ctx context.Context, tenantID int64, resourceType string, resourceID int64) (domain.ResourceBinding, error) {
	daoBinding, err := r.dao.GetByResource(ctx, tenantID, resourceType, resourceID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.ResourceBinding{}, domain.ErrBindingNotFound
		}
		return domain.ResourceBinding{}, err
	}
	return r.toDomain(daoBinding), nil
}

func (r *bindingRepository) List(ctx context.Context, filter domain.BindingFilter) ([]domain.ResourceBinding, error) {
	daoBindings, err := r.dao.List(ctx, r.toDAOFilter(filter))
	if err != nil {
		return nil, err
	}

	bindings := make([]domain.ResourceBinding, len(daoBindings))
	for i, daoBinding := range daoBindings {
		bindings[i] = r.toDomain(daoBinding)
	}
	return bindings, nil
}

func (r *bindingRepository) Count(ctx context.Context, filter domain.BindingFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

func (r *bindingRepository) CountByNodeID(ctx context.Context, nodeID int64) (int64, error) {
	return r.dao.CountByNodeID(ctx, nodeID)
}

func (r *bindingRepository) CountByRuleIDs(ctx context.Context, ruleIDs []int64) (map[int64]int64, error) {
	return r.dao.CountByRuleIDs(ctx, ruleIDs)
}

func (r *bindingRepository) ListByNodeIDs(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error) {
	daoBindings, err := r.dao.ListByNodeIDs(ctx, dao.NodeIDsBindingFilter{
		TenantID:     filter.TenantID,
		NodeIDs:      filter.NodeIDs,
		EnvID:        filter.EnvID,
		ResourceType: filter.ResourceType,
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	bindings := make([]domain.ResourceBinding, len(daoBindings))
	for i, daoBinding := range daoBindings {
		bindings[i] = r.toDomain(daoBinding)
	}
	return bindings, nil
}

func (r *bindingRepository) CountByNodeIDs(ctx context.Context, filter domain.NodeIDsBindingFilter) (int64, error) {
	return r.dao.CountByNodeIDs(ctx, dao.NodeIDsBindingFilter{
		TenantID:     filter.TenantID,
		NodeIDs:      filter.NodeIDs,
		EnvID:        filter.EnvID,
		ResourceType: filter.ResourceType,
	})
}

func (r *bindingRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

func (r *bindingRepository) DeleteByNodeID(ctx context.Context, nodeID int64) error {
	return r.dao.DeleteByNodeID(ctx, nodeID)
}

func (r *bindingRepository) DeleteByResource(ctx context.Context, tenantID int64, resourceType string, resourceID int64) error {
	return r.dao.DeleteByResource(ctx, tenantID, resourceType, resourceID)
}

func (r *bindingRepository) DeleteByRuleID(ctx context.Context, ruleID int64) (int64, error) {
	return r.dao.DeleteByRuleID(ctx, ruleID)
}

func (r *bindingRepository) UpdateTarget(ctx context.Context, id int64, nodeID int64, envID int64, ruleID int64) error {
	err := r.dao.UpdateTarget(ctx, id, nodeID, envID, ruleID)
	if err == mongo.ErrNoDocuments {
		return domain.ErrBindingNotFound
	}
	return err
}

func (r *bindingRepository) toDAO(binding domain.ResourceBinding) dao.Binding {
	return dao.Binding{
		ID:           binding.ID,
		NodeID:       binding.NodeID,
		EnvID:        binding.EnvID,
		ResourceType: binding.ResourceType,
		ResourceID:   binding.ResourceID,
		TenantID:     binding.TenantID,
		BindType:     binding.BindType,
		RuleID:       binding.RuleID,
	}
}

func (r *bindingRepository) toDomain(daoBinding dao.Binding) domain.ResourceBinding {
	return domain.ResourceBinding{
		ID:           daoBinding.ID,
		NodeID:       daoBinding.NodeID,
		EnvID:        daoBinding.EnvID,
		ResourceType: daoBinding.ResourceType,
		ResourceID:   daoBinding.ResourceID,
		TenantID:     daoBinding.TenantID,
		BindType:     daoBinding.BindType,
		RuleID:       daoBinding.RuleID,
		CreateTime:   time.UnixMilli(daoBinding.Ctime),
	}
}

func (r *bindingRepository) toDAOFilter(filter domain.BindingFilter) dao.BindingFilter {
	return dao.BindingFilter{
		TenantID:     filter.TenantID,
		NodeID:       filter.NodeID,
		EnvID:        filter.EnvID,
		ResourceType: filter.ResourceType,
		ResourceID:   filter.ResourceID,
		BindType:     filter.BindType,
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	}
}
