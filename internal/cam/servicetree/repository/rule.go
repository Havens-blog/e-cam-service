package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// RuleRepository 绑定规则仓储接口
type RuleRepository interface {
	Create(ctx context.Context, rule domain.BindingRule) (int64, error)
	Update(ctx context.Context, rule domain.BindingRule) error
	GetByID(ctx context.Context, id int64) (domain.BindingRule, error)
	List(ctx context.Context, filter domain.RuleFilter) ([]domain.BindingRule, error)
	ListEnabled(ctx context.Context, tenantID string) ([]domain.BindingRule, error)
	Count(ctx context.Context, filter domain.RuleFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByNodeID(ctx context.Context, nodeID int64) error
}

type ruleRepository struct {
	dao dao.RuleDAO
}

// NewRuleRepository 创建规则仓储
func NewRuleRepository(dao dao.RuleDAO) RuleRepository {
	return &ruleRepository{dao: dao}
}

func (r *ruleRepository) Create(ctx context.Context, rule domain.BindingRule) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(rule))
}

func (r *ruleRepository) Update(ctx context.Context, rule domain.BindingRule) error {
	return r.dao.Update(ctx, r.toDAO(rule))
}

func (r *ruleRepository) GetByID(ctx context.Context, id int64) (domain.BindingRule, error) {
	daoRule, err := r.dao.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.BindingRule{}, domain.ErrRuleNotFound
		}
		return domain.BindingRule{}, err
	}
	return r.toDomain(daoRule), nil
}

func (r *ruleRepository) List(ctx context.Context, filter domain.RuleFilter) ([]domain.BindingRule, error) {
	daoRules, err := r.dao.List(ctx, r.toDAOFilter(filter))
	if err != nil {
		return nil, err
	}

	rules := make([]domain.BindingRule, len(daoRules))
	for i, daoRule := range daoRules {
		rules[i] = r.toDomain(daoRule)
	}
	return rules, nil
}

func (r *ruleRepository) ListEnabled(ctx context.Context, tenantID string) ([]domain.BindingRule, error) {
	daoRules, err := r.dao.ListEnabled(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	rules := make([]domain.BindingRule, len(daoRules))
	for i, daoRule := range daoRules {
		rules[i] = r.toDomain(daoRule)
	}
	return rules, nil
}

func (r *ruleRepository) Count(ctx context.Context, filter domain.RuleFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

func (r *ruleRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

func (r *ruleRepository) DeleteByNodeID(ctx context.Context, nodeID int64) error {
	return r.dao.DeleteByNodeID(ctx, nodeID)
}

func (r *ruleRepository) toDAO(rule domain.BindingRule) dao.Rule {
	conditions := make([]dao.RuleCondition, len(rule.Conditions))
	for i, c := range rule.Conditions {
		conditions[i] = dao.RuleCondition{
			Field:    c.Field,
			Operator: c.Operator,
			Value:    c.Value,
		}
	}

	return dao.Rule{
		ID:          rule.ID,
		NodeID:      rule.NodeID,
		EnvID:       rule.EnvID,
		Name:        rule.Name,
		TenantID:    rule.TenantID,
		Priority:    rule.Priority,
		Conditions:  conditions,
		Enabled:     rule.Enabled,
		Description: rule.Description,
	}
}

func (r *ruleRepository) toDomain(daoRule dao.Rule) domain.BindingRule {
	conditions := make([]domain.RuleCondition, len(daoRule.Conditions))
	for i, c := range daoRule.Conditions {
		conditions[i] = domain.RuleCondition{
			Field:    c.Field,
			Operator: c.Operator,
			Value:    c.Value,
		}
	}

	return domain.BindingRule{
		ID:          daoRule.ID,
		NodeID:      daoRule.NodeID,
		EnvID:       daoRule.EnvID,
		Name:        daoRule.Name,
		TenantID:    daoRule.TenantID,
		Priority:    daoRule.Priority,
		Conditions:  conditions,
		Enabled:     daoRule.Enabled,
		Description: daoRule.Description,
		CreateTime:  time.UnixMilli(daoRule.Ctime),
		UpdateTime:  time.UnixMilli(daoRule.Utime),
	}
}

func (r *ruleRepository) toDAOFilter(filter domain.RuleFilter) dao.RuleFilter {
	return dao.RuleFilter{
		TenantID: filter.TenantID,
		NodeID:   filter.NodeID,
		Enabled:  filter.Enabled,
		Name:     filter.Name,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}
}
