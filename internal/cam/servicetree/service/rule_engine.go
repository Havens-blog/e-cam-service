package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

const (
	// instanceScanBatchSize 规则引擎扫描资产的单批大小
	instanceScanBatchSize = 5000
	// maxScanInstances 单次执行扫描的资产上限（防御性护栏，避免异常数据量下无限翻页）
	maxScanInstances = 500000
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
	MatchInstance(ctx context.Context, tenantID int64, instance domain.Instance) (*stdomain.RuleMatchResult, error)
	ExecuteRules(ctx context.Context, tenantID int64) (int64, error)
	// DryRunRules 规则试运行：按临时条件预览命中资产清单，只读不落库
	DryRunRules(ctx context.Context, tenantID int64, req stdomain.DryRunRequest) (*stdomain.DryRunResult, error)

	// ExecuteRulesAsync 异步执行规则（资产同步完成后的事件驱动挂点）：
	// goroutine + panic recover + 超时 + 失败仅日志，不阻塞调用方；同租户 in-flight 去重
	ExecuteRulesAsync(tenantID int64)
}

type ruleEngineService struct {
	ruleRepo     repository.RuleRepository
	bindingRepo  repository.BindingRepository
	nodeRepo     repository.NodeRepository
	instanceRepo camrepo.InstanceRepository
	logger       *elog.Component
	// asyncRunning 同租户规则异步执行 in-flight 标记（tenantID -> struct{}），
	// 防止多次同步完成事件触发并发重复执行
	asyncRunning sync.Map
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
	s.fillNodeNames(ctx, rules)
	return rules, total, nil
}

// fillNodeNames 批量回填目标节点名称（一次 $in 查询建映射，避免 N+1；失败仅告警不阻塞列表）
func (s *ruleEngineService) fillNodeNames(ctx context.Context, rules []stdomain.BindingRule) {
	idSet := make(map[int64]struct{}, len(rules))
	ids := make([]int64, 0, len(rules))
	for _, r := range rules {
		if r.NodeID > 0 {
			if _, ok := idSet[r.NodeID]; !ok {
				idSet[r.NodeID] = struct{}{}
				ids = append(ids, r.NodeID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}

	nodes, err := s.nodeRepo.GetByIDs(ctx, ids)
	if err != nil {
		s.logger.Warn("批量查询规则目标节点失败", elog.FieldErr(err))
		return
	}
	nameByID := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		nameByID[n.ID] = n.Name
	}
	for i := range rules {
		if name, ok := nameByID[rules[i].NodeID]; ok {
			rules[i].NodeName = name
		}
	}
}

// MatchInstance 匹配实例到规则
func (s *ruleEngineService) MatchInstance(ctx context.Context, tenantID int64, instance domain.Instance) (*stdomain.RuleMatchResult, error) {
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
func (s *ruleEngineService) ExecuteRules(ctx context.Context, tenantID int64) (int64, error) {
	s.logger.Info("开始执行规则匹配", elog.Int64("tenantID", tenantID))

	// 1. 获取所有启用的规则，按优先级排序
	rules, err := s.ruleRepo.ListEnabled(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("获取规则列表失败: %w", err)
	}
	if len(rules) == 0 {
		s.logger.Info("无启用的规则", elog.Int64("tenantID", tenantID))
		return 0, nil
	}

	// 2. 获取所有实例（资产取数路径与 DryRunRules 共用）
	instances, err := s.listTenantInstances(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("获取实例列表失败: %w", err)
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
	// key: resourceID（对齐 DB 资源级唯一键 tenant_id+resource_type+resource_id，dao/init.go
	// initBindingIndexes：一个资源仅一条绑定。资源级去重同时保证两件事：
	// 1) 幂等——重复执行（同步后自动触发/手动按钮）零重复绑定；
	// 2) 手动优先 locked——已手动绑定（任意环境）的资产规则执行整体跳过，不会被规则覆盖）
	boundResources := make(map[int64]bool, len(existingBindings))
	for _, b := range existingBindings {
		boundResources[b.ResourceID] = true
	}

	// 4. 遍历未绑定的实例，匹配规则
	var newBindings []stdomain.ResourceBinding
	matchCounts := make(map[int64]int64) // 规则ID -> 本次执行新增匹配绑定数
	for _, instance := range instances {
		// 已绑定（任意环境、manual/rule）的资产跳过：手动优先 locked + 幂等
		if boundResources[instance.ID] {
			continue
		}
		// 按优先级匹配规则
		for _, rule := range rules {
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
				matchCounts[rule.ID]++
				boundResources[instance.ID] = true // 同批次内去重
				break                              // 匹配到第一个规则后停止
			}
		}
	}

	// 5. 执行结果落库（规则级统计：执行时间 + 本次新增匹配数，未命中的规则也记本次执行）
	// 统计写失败仅告警，不阻塞绑定主流程
	now := time.Now()
	for _, rule := range rules {
		if err := s.ruleRepo.UpdateExecutionStats(ctx, rule.ID, now, matchCounts[rule.ID]); err != nil {
			s.logger.Warn("规则执行统计落库失败",
				elog.Int64("ruleID", rule.ID),
				elog.FieldErr(err))
		}
	}

	if len(newBindings) == 0 {
		s.logger.Info("无新的匹配绑定", elog.Int64("tenantID", tenantID))
		return 0, nil
	}

	// 6. 批量创建绑定
	count, err := s.bindingRepo.CreateBatch(ctx, newBindings)
	if err != nil {
		return 0, fmt.Errorf("批量创建绑定失败: %w", err)
	}

	s.logger.Info("规则匹配完成",
		elog.Int64("tenantID", tenantID),
		elog.Int("ruleCount", len(rules)),
		elog.Int("instanceCount", len(instances)),
		elog.Int64("newBindingCount", count),
	)

	return count, nil
}

// listTenantInstances 拉取租户全部资产实例（ExecuteRules 与 DryRunRules 共用的取数路径）。
// 分页累积取全量：原先固定 Limit 10000，租户资产超过该值时静默漏扫（2w+ 资产时每次
// 只匹配其中 1w 条，且 DAO 排序为 ctime 倒序——非唯一键，每次同步触发扫到的切片不同，
// 绑定数会随执行次数逐批爬升而非一次收敛）。跨批可能重复，按实例 ID 去重。
func (s *ruleEngineService) listTenantInstances(ctx context.Context, tenantID int64) ([]domain.Instance, error) {
	all := make([]domain.Instance, 0, instanceScanBatchSize)
	seen := make(map[int64]bool)
	for offset := int64(0); offset < maxScanInstances; offset += instanceScanBatchSize {
		batch, err := s.instanceRepo.List(ctx, domain.InstanceFilter{
			TenantID: tenantID,
			Offset:   offset,
			Limit:    instanceScanBatchSize,
		})
		if err != nil {
			return nil, err
		}
		for _, inst := range batch {
			if seen[inst.ID] {
				continue
			}
			seen[inst.ID] = true
			all = append(all, inst)
		}
		if int64(len(batch)) < instanceScanBatchSize {
			break // 末页
		}
	}
	return all, nil
}

// DryRunRules 规则试运行：按临时条件（不落库的规则体）预览命中资产清单。
// 只读：不创建绑定、不改规则统计；匹配复用 matchRule/matchCondition，
// 资产遍历复用 ExecuteRules 的取数路径，命中清单截断到 stdomain.MaxDryRunItems。
func (s *ruleEngineService) DryRunRules(ctx context.Context, tenantID int64, req stdomain.DryRunRequest) (*stdomain.DryRunResult, error) {
	if len(req.Conditions) == 0 {
		return nil, stdomain.ErrRuleConditionsEmpty
	}

	instances, err := s.listTenantInstances(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取实例列表失败: %w", err)
	}

	// 当前绑定关系（只读查询），用于如实标注命中资产的现状（手动绑节点/规则绑节点）
	bindings, err := s.bindingRepo.List(ctx, stdomain.BindingFilter{
		TenantID:     tenantID,
		ResourceType: stdomain.ResourceTypeInstance,
		Limit:        100000,
	})
	if err != nil {
		return nil, fmt.Errorf("获取已绑定资源失败: %w", err)
	}
	bindingsByResource := make(map[int64][]stdomain.ResourceBinding)
	for _, b := range bindings {
		bindingsByResource[b.ResourceID] = append(bindingsByResource[b.ResourceID], b)
	}

	// 临时规则体仅携带条件，与 ExecuteRules 走同一套匹配实现
	tmpRule := stdomain.BindingRule{NodeID: req.NodeID, EnvID: req.EnvID, Conditions: req.Conditions}

	result := &stdomain.DryRunResult{Items: make([]stdomain.DryRunMatchItem, 0)}
	for _, inst := range instances {
		if !s.matchRule(inst, tmpRule) {
			continue
		}
		result.Total++
		if result.Total > stdomain.MaxDryRunItems {
			continue // 超出上限只计数不再填充，保护响应体积
		}
		item := stdomain.DryRunMatchItem{
			ResourceID: inst.ID,
			AssetID:    inst.AssetID,
			AssetName:  inst.AssetName,
			Provider:   inst.GetStringAttribute("provider"),
			Region:     inst.GetStringAttribute("region"),
		}
		if b, ok := currentBinding(bindingsByResource[inst.ID], req.EnvID); ok {
			item.BindStatus = b.BindType
			item.BoundNodeID = b.NodeID
			item.BoundEnvID = b.EnvID
			item.BoundRuleID = b.RuleID
		} else {
			item.BindStatus = stdomain.BindStatusUnbound
		}
		result.Items = append(result.Items, item)
	}
	result.Capped = result.Total > int64(len(result.Items))

	s.fillBoundNodeNames(ctx, result.Items)
	return result, nil
}

// currentBinding 取资产的当前绑定：指定环境时取该环境下的绑定（视为该环境口径的现状），
// 否则取任一绑定（优先规则绑定）。无匹配返回 false。
func currentBinding(bindings []stdomain.ResourceBinding, envID int64) (stdomain.ResourceBinding, bool) {
	var fallback *stdomain.ResourceBinding
	for i := range bindings {
		b := &bindings[i]
		if envID > 0 && b.EnvID != envID {
			continue
		}
		if b.BindType == stdomain.BindTypeRule {
			return *b, true
		}
		if fallback == nil {
			fallback = b
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return stdomain.ResourceBinding{}, false
}

// fillBoundNodeNames 批量回填命中资产已绑节点名称（一次 $in 查询建映射，避免 N+1；失败仅告警不阻塞）
func (s *ruleEngineService) fillBoundNodeNames(ctx context.Context, items []stdomain.DryRunMatchItem) {
	idSet := make(map[int64]struct{}, len(items))
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.BoundNodeID > 0 {
			if _, ok := idSet[item.BoundNodeID]; !ok {
				idSet[item.BoundNodeID] = struct{}{}
				ids = append(ids, item.BoundNodeID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}

	nodes, err := s.nodeRepo.GetByIDs(ctx, ids)
	if err != nil {
		s.logger.Warn("批量查询命中资产已绑节点失败", elog.FieldErr(err))
		return
	}
	nameByID := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		nameByID[n.ID] = n.Name
	}
	for i := range items {
		if name, ok := nameByID[items[i].BoundNodeID]; ok {
			items[i].BoundNodeName = name
		}
	}
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
	case "region":
		// region 由采集侧落入 Instance attributes（sync_* 各资产均写 attributes["region"]），
		// 与 node_asset.go 的 GetStringAttribute("region") 取值口径一致；缺失返回空串不匹配
		return instance.GetStringAttribute("region")
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
// Eq/Contains/Regex 委托共享 cam/domain.MatchValue（语义单点定义）；
// Contains 由此由大小写敏感放宽为不敏感（唯一行为变化，已确认）。
// Ne/In/NotIn/Exists 为 servicetree 独有操作符，保留在原处。
func (s *ruleEngineService) compareValue(actual, operator, expected string) bool {
	switch operator {
	case stdomain.OperatorEq:
		return domain.MatchValue("equals", actual, expected)
	case stdomain.OperatorContains:
		return domain.MatchValue("contains", actual, expected)
	case stdomain.OperatorRegex:
		return domain.MatchValue("regex", actual, expected)
	case stdomain.OperatorNe:
		return actual != expected
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
