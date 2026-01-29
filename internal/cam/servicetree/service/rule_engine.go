package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// RuleEngineService 规则引擎服务接口
type RuleEngineService interface {
	// 规则管理
	CreateRule(ctx context.Context, rule stdomain.BindingRule) (int64, error)
	UpdateRule(ctx context.Context, rule stdomain.BindingRule) error
	DeleteRule(ctx context.Context, id int64) error
	GetRule(ctx context.Context, id int64) (stdomain.BindingRule, error)
	ListRules(ctx context.Context, filter stdomain.RuleFilter) ([]stdomain.BindingRule, int64, error)

	// 规则匹配
	MatchInstance(ctx context.Context, tenantID string, instance domain.Instance) (*stdomain.RuleMatchResult, error)
	ExecuteRules(ctx context.Context, tenantID string) (int64, error)
}

type ruleEngineService struct {
	ruleRepo     repository.RuleRepository
	bindingRepo  repository.BindingRepository
	nodeRepo     repository.NodeRepository
	instanceRepo camrepo.InstanceRepository
	logger       *elog.Component
}

// NewRuleEngineService 创建规则引擎服务
func NewRuleEngineService(
	ruleRepo repository.RuleRepository,
	bindingRepo repository.BindingRepository,
	nodeRepo repository.NodeRepository,
	instanceRepo camrepo.InstanceRepository,
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

func (s *ruleEngineService) CreateRule(ctx context.Context, rule stdomain.BindingRule) (int64, error) {
	if err := rule.Validate(); err != nil {
		return 0, err
	}

	// 验证节点存在
	_, err := s.nodeRepo.GetByID(ctx, rule.NodeID)
	if err != nil {
		return 0, stdomain.ErrNodeNotFound
	}

	return s.ruleRepo.Create(ctx, rule)
}

func (s *ruleEngineService) UpdateRule(ctx context.Context, rule stdomain.BindingRule) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	return s.ruleRepo.Update(ctx, rule)
}

func (s *ruleEngineService) DeleteRule(ctx context.Context, id int64) error {
	// 删除规则关联的绑定
	if err := s.bindingRepo.DeleteByRuleID(ctx, id); err != nil {
		s.logger.Warn("删除规则关联绑定失败", elog.Int64("ruleID", id), elog.FieldErr(err))
	}
	return s.ruleRepo.Delete(ctx, id)
}

func (s *ruleEngineService) GetRule(ctx context.Context, id int64) (stdomain.BindingRule, error) {
	return s.ruleRepo.GetByID(ctx, id)
}

func (s *ruleEngineService) ListRules(ctx context.Context, filter stdomain.RuleFilter) ([]stdomain.BindingRule, int64, error) {
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

// MatchInstance 匹配实例到规则
func (s *ruleEngineService) MatchInstance(ctx context.Context, tenantID string, instance domain.Instance) (*stdomain.RuleMatchResult, error) {
	rules, err := s.ruleRepo.ListEnabled(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	for _, rule := range rules {
		if s.matchRule(instance, rule) {
			return &stdomain.RuleMatchResult{
				RuleID:     rule.ID,
				NodeID:     rule.NodeID,
				ResourceID: instance.ID,
				Matched:    true,
				Reason:     "匹配规则: " + rule.Name,
			}, nil
		}
	}

	return &stdomain.RuleMatchResult{
		ResourceID: instance.ID,
		Matched:    false,
		Reason:     "无匹配规则",
	}, nil
}

// ExecuteRules 执行所有规则，自动绑定资源到规则指定的环境
func (s *ruleEngineService) ExecuteRules(ctx context.Context, tenantID string) (int64, error) {
	s.logger.Info("开始执行规则匹配", elog.String("tenantID", tenantID))

	// 1. 获取所有启用的规则，按优先级排序
	rules, err := s.ruleRepo.ListEnabled(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("获取规则列表失败: %w", err)
	}
	if len(rules) == 0 {
		s.logger.Info("无启用的规则", elog.String("tenantID", tenantID))
		return 0, nil
	}

	// 2. 获取所有实例
	instances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{
		TenantID: tenantID,
		Limit:    10000, // 分批处理大量数据时可优化
	})
	if err != nil {
		return 0, fmt.Errorf("获取实例列表失败: %w", err)
	}
	if len(instances) == 0 {
		s.logger.Info("无实例数据", elog.String("tenantID", tenantID))
		return 0, nil
	}

	// 3. 获取已绑定的资源ID集合 (按环境分组)
	existingBindings, err := s.bindingRepo.List(ctx, stdomain.BindingFilter{
		TenantID:     tenantID,
		ResourceType: stdomain.ResourceTypeInstance,
		Limit:        100000,
	})
	if err != nil {
		return 0, fmt.Errorf("获取已绑定资源失败: %w", err)
	}
	// key: "envID-resourceID"
	boundResources := make(map[string]bool)
	for _, b := range existingBindings {
		key := fmt.Sprintf("%d-%d", b.EnvID, b.ResourceID)
		boundResources[key] = true
	}

	// 4. 遍历未绑定的实例，匹配规则
	var newBindings []stdomain.ResourceBinding
	for _, instance := range instances {
		// 按优先级匹配规则
		for _, rule := range rules {
			// 检查该实例在该环境下是否已绑定
			key := fmt.Sprintf("%d-%d", rule.EnvID, instance.ID)
			if boundResources[key] {
				continue
			}

			if s.matchRule(instance, rule) {
				newBindings = append(newBindings, stdomain.ResourceBinding{
					NodeID:       rule.NodeID,
					EnvID:        rule.EnvID,
					ResourceType: stdomain.ResourceTypeInstance,
					ResourceID:   instance.ID,
					TenantID:     tenantID,
					BindType:     stdomain.BindTypeRule,
					RuleID:       rule.ID,
				})
				// 标记为已绑定，避免同一实例在同一环境被多个规则绑定
				boundResources[key] = true
				break // 匹配到第一个规则后停止
			}
		}
	}

	if len(newBindings) == 0 {
		s.logger.Info("无新的匹配绑定", elog.String("tenantID", tenantID))
		return 0, nil
	}

	// 5. 批量创建绑定
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

// matchRule 检查实例是否匹配规则
func (s *ruleEngineService) matchRule(instance domain.Instance, rule stdomain.BindingRule) bool {
	for _, cond := range rule.Conditions {
		if !s.matchCondition(instance, cond) {
			return false
		}
	}
	return true
}

// matchCondition 检查单个条件
func (s *ruleEngineService) matchCondition(instance domain.Instance, cond stdomain.RuleCondition) bool {
	value := s.getFieldValue(instance, cond.Field)
	return s.compareValue(value, cond.Operator, cond.Value)
}

// getFieldValue 获取实例字段值
func (s *ruleEngineService) getFieldValue(instance domain.Instance, field string) string {
	switch field {
	case "name":
		return instance.AssetName
	case "asset_id":
		return instance.AssetID
	case "model_uid":
		return instance.ModelUID
	default:
		// 处理 attributes.xxx 和 tag.xxx
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

// compareValue 比较值
func (s *ruleEngineService) compareValue(actual, operator, expected string) bool {
	switch operator {
	case stdomain.OperatorEq:
		return actual == expected
	case stdomain.OperatorNe:
		return actual != expected
	case stdomain.OperatorContains:
		return strings.Contains(actual, expected)
	case stdomain.OperatorRegex:
		matched, _ := regexp.MatchString(expected, actual)
		return matched
	case stdomain.OperatorIn:
		values := strings.Split(expected, ",")
		for _, v := range values {
			if strings.TrimSpace(v) == actual {
				return true
			}
		}
		return false
	case stdomain.OperatorNotIn:
		values := strings.Split(expected, ",")
		for _, v := range values {
			if strings.TrimSpace(v) == actual {
				return false
			}
		}
		return true
	case stdomain.OperatorExists:
		return actual != ""
	default:
		return false
	}
}
