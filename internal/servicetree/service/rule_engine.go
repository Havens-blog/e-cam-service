package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// RuleEngineService 规则引擎服务接口
type RuleEngineService interface {
	CreateRule(ctx context.Context, rule domain.BindingRule) (int64, error)
	UpdateRule(ctx context.Context, rule domain.BindingRule) error
	DeleteRule(ctx context.Context, id int64) error
	GetRule(ctx context.Context, id int64) (domain.BindingRule, error)
	ListRules(ctx context.Context, filter domain.RuleFilter) ([]domain.BindingRule, int64, error)
	MatchInstance(ctx context.Context, tenantID string, instance assetdomain.Instance) (*domain.RuleMatchResult, error)
	ExecuteRules(ctx context.Context, tenantID string) (int64, error)
}

type ruleEngineService struct {
	ruleRepo     repository.RuleRepository
	bindingRepo  repository.BindingRepository
	nodeRepo     repository.NodeRepository
	instanceRepo assetrepo.InstanceRepository
	logger       *elog.Component
}

// NewRuleEngineService 创建规则引擎服务
func NewRuleEngineService(
	ruleRepo repository.RuleRepository,
	bindingRepo repository.BindingRepository,
	nodeRepo repository.NodeRepository,
	instanceRepo assetrepo.InstanceRepository,
	logger *elog.Component,
) RuleEngineService {
	return &ruleEngineService{
		ruleRepo:     ruleRepo,
		bindingRepo:  bindingRepo,
		nodeRepo:     nodeRepo,
		instanceRepo: instanceRepo,
		logger:       logger,
	}
}

func (s *ruleEngineService) CreateRule(ctx context.Context, rule domain.BindingRule) (int64, error) {
	if err := rule.Validate(); err != nil {
		return 0, err
	}

	_, err := s.nodeRepo.GetByID(ctx, rule.NodeID)
	if err != nil {
		return 0, domain.ErrNodeNotFound
	}

	return s.ruleRepo.Create(ctx, rule)
}

func (s *ruleEngineService) UpdateRule(ctx context.Context, rule domain.BindingRule) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	return s.ruleRepo.Update(ctx, rule)
}

func (s *ruleEngineService) DeleteRule(ctx context.Context, id int64) error {
	if err := s.bindingRepo.DeleteByRuleID(ctx, id); err != nil {
		s.logger.Warn("删除规则关联绑定失败", elog.Int64("ruleID", id), elog.FieldErr(err))
	}
	return s.ruleRepo.Delete(ctx, id)
}

func (s *ruleEngineService) GetRule(ctx context.Context, id int64) (domain.BindingRule, error) {
	return s.ruleRepo.GetByID(ctx, id)
}

func (s *ruleEngineService) ListRules(ctx context.Context, filter domain.RuleFilter) ([]domain.BindingRule, int64, error) {
	rules, err := s.ruleRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.ruleRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return rules, total, nil
}

func (s *ruleEngineService) MatchInstance(ctx context.Context, tenantID string, instance assetdomain.Instance) (*domain.RuleMatchResult, error) {
	rules, err := s.ruleRepo.ListEnabled(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	for _, rule := range rules {
		if s.matchRule(instance, rule) {
			return &domain.RuleMatchResult{
				RuleID:     rule.ID,
				NodeID:     rule.NodeID,
				ResourceID: instance.ID,
				Matched:    true,
				Reason:     "匹配规则: " + rule.Name,
			}, nil
		}
	}

	return &domain.RuleMatchResult{
		ResourceID: instance.ID,
		Matched:    false,
		Reason:     "无匹配规则",
	}, nil
}

func (s *ruleEngineService) ExecuteRules(ctx context.Context, tenantID string) (int64, error) {
	s.logger.Info("开始执行规则匹配", elog.String("tenantID", tenantID))

	rules, err := s.ruleRepo.ListEnabled(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("获取规则列表失败: %w", err)
	}
	if len(rules) == 0 {
		s.logger.Info("无启用的规则", elog.String("tenantID", tenantID))
		return 0, nil
	}

	instances, err := s.instanceRepo.List(ctx, assetdomain.InstanceFilter{
		TenantID: tenantID,
		Limit:    10000,
	})
	if err != nil {
		return 0, fmt.Errorf("获取实例列表失败: %w", err)
	}
	if len(instances) == 0 {
		s.logger.Info("无实例数据", elog.String("tenantID", tenantID))
		return 0, nil
	}

	existingBindings, err := s.bindingRepo.List(ctx, domain.BindingFilter{
		TenantID:     tenantID,
		ResourceType: domain.ResourceTypeInstance,
		Limit:        100000,
	})
	if err != nil {
		return 0, fmt.Errorf("获取已绑定资源失败: %w", err)
	}

	boundResources := make(map[string]bool)
	for _, b := range existingBindings {
		key := fmt.Sprintf("%d-%d", b.EnvID, b.ResourceID)
		boundResources[key] = true
	}

	var newBindings []domain.ResourceBinding
	for _, instance := range instances {
		for _, rule := range rules {
			key := fmt.Sprintf("%d-%d", rule.EnvID, instance.ID)
			if boundResources[key] {
				continue
			}

			if s.matchRule(instance, rule) {
				newBindings = append(newBindings, domain.ResourceBinding{
					NodeID:       rule.NodeID,
					EnvID:        rule.EnvID,
					ResourceType: domain.ResourceTypeInstance,
					ResourceID:   instance.ID,
					TenantID:     tenantID,
					BindType:     domain.BindTypeRule,
					RuleID:       rule.ID,
				})
				boundResources[key] = true
				break
			}
		}
	}

	if len(newBindings) == 0 {
		s.logger.Info("无新的匹配绑定", elog.String("tenantID", tenantID))
		return 0, nil
	}

	count, err := s.bindingRepo.CreateBatch(ctx, newBindings)
	if err != nil {
		return 0, fmt.Errorf("批量创建绑定失败: %w", err)
	}

	s.logger.Info("规则匹配完成",
		elog.String("tenantID", tenantID),
		elog.Int("ruleCount", len(rules)),
		elog.Int("instanceCount", len(instances)),
		elog.Int64("newBindingCount", count),
	)

	return count, nil
}

func (s *ruleEngineService) matchRule(instance assetdomain.Instance, rule domain.BindingRule) bool {
	for _, cond := range rule.Conditions {
		if !s.matchCondition(instance, cond) {
			return false
		}
	}
	return true
}

func (s *ruleEngineService) matchCondition(instance assetdomain.Instance, cond domain.RuleCondition) bool {
	value := s.getFieldValue(instance, cond.Field)
	return s.compareValue(value, cond.Operator, cond.Value)
}

func (s *ruleEngineService) getFieldValue(instance assetdomain.Instance, field string) string {
	switch field {
	case "name":
		return instance.AssetName
	case "asset_id":
		return instance.AssetID
	case "model_uid":
		return instance.ModelUID
	default:
		if strings.HasPrefix(field, "attributes.") {
			key := strings.TrimPrefix(field, "attributes.")
			if val, ok := instance.Attributes[key]; ok {
				if str, ok := val.(string); ok {
					return str
				}
			}
		} else if strings.HasPrefix(field, "tag.") {
			tagKey := strings.TrimPrefix(field, "tag.")
			if tags, ok := instance.Attributes["tags"].(map[string]any); ok {
				if val, ok := tags[tagKey]; ok {
					if str, ok := val.(string); ok {
						return str
					}
				}
			}
		}
	}
	return ""
}

func (s *ruleEngineService) compareValue(actual, operator, expected string) bool {
	switch operator {
	case domain.OperatorEq:
		return actual == expected
	case domain.OperatorNe:
		return actual != expected
	case domain.OperatorContains:
		return strings.Contains(actual, expected)
	case domain.OperatorRegex:
		matched, _ := regexp.MatchString(expected, actual)
		return matched
	case domain.OperatorIn:
		values := strings.Split(expected, ",")
		for _, v := range values {
			if strings.TrimSpace(v) == actual {
				return true
			}
		}
		return false
	case domain.OperatorNotIn:
		values := strings.Split(expected, ",")
		for _, v := range values {
			if strings.TrimSpace(v) == actual {
				return false
			}
		}
		return true
	case domain.OperatorExists:
		return actual != ""
	default:
		return false
	}
}
