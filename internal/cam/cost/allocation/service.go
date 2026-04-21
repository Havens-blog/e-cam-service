// Package allocation 多维度成本分摊引擎
package allocation

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
)

const (
	maxDimensionCombos = 5
	ratioTolerance     = 0.01
	ratioTarget        = 100.0
)

// validDimTypes 有效的维度类型集合
var validDimTypes = map[string]bool{
	costdomain.DimDepartment:    true,
	costdomain.DimResourceGroup: true,
	costdomain.DimProject:       true,
	costdomain.DimTag:           true,
	costdomain.DimCloudAccount:  true,
	costdomain.DimRegion:        true,
	costdomain.DimServiceType:   true,
}

// DimensionCostAllocation 按维度查询的分摊结果
type DimensionCostAllocation struct {
	DimType     string                      `json:"dim_type"`
	DimValue    string                      `json:"dim_value"`
	Period      string                      `json:"period"`
	Allocations []costdomain.CostAllocation `json:"allocations"`
}

// NodeCostAllocation 按服务树节点查询的分摊结果
type NodeCostAllocation struct {
	NodeID      int64                       `json:"node_id"`
	Period      string                      `json:"period"`
	Allocations []costdomain.CostAllocation `json:"allocations"`
}

// AllocationTreeNode 分摊树形节点
type AllocationTreeNode struct {
	NodeID      string                `json:"node_id"`
	NodeName    string                `json:"node_name"`
	DimType     string                `json:"dim_type"`
	TotalAmount float64               `json:"total_amount"`
	Children    []*AllocationTreeNode `json:"children,omitempty"`
}

// AllocationService 成本分摊服务
type AllocationService struct {
	allocationDAO repository.AllocationDAO
	billDAO       repository.BillDAO
	logger        *elog.Component
}

// NewAllocationService 创建成本分摊服务
func NewAllocationService(
	allocationDAO repository.AllocationDAO,
	billDAO repository.BillDAO,
	logger *elog.Component,
) *AllocationService {
	return &AllocationService{
		allocationDAO: allocationDAO,
		billDAO:       billDAO,
		logger:        logger,
	}
}

// Logger 返回日志组件（供 handler 异步场景使用）
func (s *AllocationService) Logger() *elog.Component {
	return s.logger
}

// CreateAllocationRule 创建分摊规则
func (s *AllocationService) CreateAllocationRule(ctx context.Context, rule costdomain.AllocationRule) (int64, error) {
	if err := s.validateRule(rule); err != nil {
		return 0, err
	}

	now := time.Now().UnixMilli()
	rule.Status = "active"
	rule.CreateTime = now
	rule.UpdateTime = now

	return s.allocationDAO.CreateRule(ctx, rule)
}

// UpdateAllocationRule 更新分摊规则
func (s *AllocationService) UpdateAllocationRule(ctx context.Context, rule costdomain.AllocationRule) error {
	if err := s.validateRule(rule); err != nil {
		return err
	}

	rule.UpdateTime = time.Now().UnixMilli()
	return s.allocationDAO.UpdateRule(ctx, rule)
}

// SetDefaultAllocationPolicy 设置默认分摊策略
func (s *AllocationService) SetDefaultAllocationPolicy(ctx context.Context, policy costdomain.DefaultAllocationPolicy) error {
	now := time.Now().UnixMilli()
	policy.CreateTime = now
	policy.UpdateTime = now
	return s.allocationDAO.SaveDefaultPolicy(ctx, policy)
}

// AllocateCosts 执行成本分摊计算
func (s *AllocationService) AllocateCosts(ctx context.Context, tenantID string, period string) error {
	// 1. Delete existing allocations for this period
	if err := s.allocationDAO.DeleteAllocationsByPeriod(ctx, tenantID, period); err != nil {
		return fmt.Errorf("delete existing allocations: %w", err)
	}

	// 2. Get all active rules sorted by priority
	rules, err := s.allocationDAO.ListActiveRules(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list active rules: %w", err)
	}

	// 3. Get default policy
	defaultPolicy, err := s.allocationDAO.GetDefaultPolicy(ctx, tenantID)
	hasDefaultPolicy := err == nil && defaultPolicy.TargetID != ""

	// 4. Get all bills for the period
	startDate := period + "-01"
	endDate := s.periodEndDate(period)
	bills, err := s.billDAO.ListUnifiedBills(ctx, repository.UnifiedBillFilter{
		TenantID:  tenantID,
		StartDate: startDate,
		EndDate:   endDate,
	})
	if err != nil {
		return fmt.Errorf("list bills: %w", err)
	}

	var allocations []costdomain.CostAllocation
	now := time.Now().UnixMilli()

	for _, bill := range bills {
		matched := false

		for _, rule := range rules {
			allocs := s.matchAndAllocate(bill, rule, period, now)
			if len(allocs) > 0 {
				allocations = append(allocations, allocs...)
				matched = true
				break // first matching rule wins (priority order)
			}
		}

		if !matched {
			alloc := s.createUnmatchedAllocation(bill, period, now, hasDefaultPolicy, defaultPolicy)
			allocations = append(allocations, alloc)
		}
	}

	// 5. Batch insert allocations
	if len(allocations) > 0 {
		if _, err := s.allocationDAO.InsertAllocations(ctx, allocations); err != nil {
			return fmt.Errorf("insert allocations: %w", err)
		}
	}

	s.logger.Info("cost allocation completed",
		elog.String("tenant_id", tenantID),
		elog.String("period", period),
		elog.Int("bill_count", len(bills)),
		elog.Int("allocation_count", len(allocations)))

	return nil
}

// GetAllocationByDimension 按维度查询分摊结果
func (s *AllocationService) GetAllocationByDimension(ctx context.Context, tenantID, dimType, dimValue, period string) (*DimensionCostAllocation, error) {
	allocs, err := s.allocationDAO.GetAllocationByDimension(ctx, tenantID, dimType, dimValue, period)
	if err != nil {
		return nil, fmt.Errorf("get allocation by dimension: %w", err)
	}
	return &DimensionCostAllocation{
		DimType:     dimType,
		DimValue:    dimValue,
		Period:      period,
		Allocations: allocs,
	}, nil
}

// GetAllocationByNode 按服务树节点查询分摊结果
func (s *AllocationService) GetAllocationByNode(ctx context.Context, tenantID string, nodeID int64, period string) (*NodeCostAllocation, error) {
	allocs, err := s.allocationDAO.GetAllocationByNode(ctx, tenantID, nodeID, period)
	if err != nil {
		return nil, fmt.Errorf("get allocation by node: %w", err)
	}
	return &NodeCostAllocation{
		NodeID:      nodeID,
		Period:      period,
		Allocations: allocs,
	}, nil
}

// GetAllocationTree 获取维度层级成本分摊树形视图
// 优先查已有分摊结果，没有则直接从账单聚合（不同步执行分摊计算，避免超时）
func (s *AllocationService) GetAllocationTree(ctx context.Context, tenantID, dimType, rootID, period string) (*AllocationTreeNode, error) {
	allocs, err := s.allocationDAO.ListAllocations(ctx, repository.AllocationFilter{
		TenantID: tenantID,
		DimType:  dimType,
		Period:   period,
	})
	if err != nil {
		return nil, fmt.Errorf("list allocations for tree: %w", err)
	}

	// 有分摊结果则用分摊结果构建树
	if len(allocs) > 0 {
		return s.buildTree(allocs, rootID, dimType), nil
	}

	// 没有分摊结果，直接从统一账单按维度聚合（aggregation pipeline，毫秒级）
	return s.buildTreeFromBills(ctx, tenantID, dimType, rootID, period)
}

// ReAllocateHistory 重新分摊历史数据
func (s *AllocationService) ReAllocateHistory(ctx context.Context, tenantID string, period string) error {
	return s.AllocateCosts(ctx, tenantID, period)
}

// ListRules 查询分摊规则列表
func (s *AllocationService) ListRules(ctx context.Context, filter repository.AllocationRuleFilter) ([]costdomain.AllocationRule, error) {
	return s.allocationDAO.ListRules(ctx, filter)
}

// DeleteRule 删除分摊规则
func (s *AllocationService) DeleteRule(ctx context.Context, id int64) error {
	return s.allocationDAO.DeleteRule(ctx, id)
}

// validateRule 校验分摊规则
func (s *AllocationService) validateRule(rule costdomain.AllocationRule) error {
	if rule.Name == "" {
		return fmt.Errorf("rule name cannot be empty")
	}

	if rule.RuleType == "dimension_combo" {
		if len(rule.DimensionCombos) == 0 {
			return fmt.Errorf("dimension combos cannot be empty")
		}
		if len(rule.DimensionCombos) > maxDimensionCombos {
			return costdomain.ErrAllocationDimExceed
		}

		var ratioSum float64
		for _, combo := range rule.DimensionCombos {
			for _, dim := range combo.Dimensions {
				if !validDimTypes[dim.DimType] {
					return costdomain.ErrAllocationDimInvalid
				}
			}
			ratioSum += combo.Ratio
		}

		if math.Abs(ratioSum-ratioTarget) > ratioTolerance {
			return costdomain.ErrAllocationRatioInvalid
		}
	}

	if rule.RuleType == "tag_mapping" {
		if rule.TagKey == "" {
			return fmt.Errorf("tag_key cannot be empty for tag_mapping rule")
		}
	}

	if rule.RuleType == "shared_ratio" {
		if rule.SharedConfig == nil {
			return fmt.Errorf("shared_config cannot be nil for shared_ratio rule")
		}
		var ratioSum float64
		for _, ratio := range rule.SharedConfig.Ratios {
			ratioSum += ratio
		}
		if math.Abs(ratioSum-ratioTarget) > ratioTolerance {
			return costdomain.ErrAllocationRatioInvalid
		}
	}

	return nil
}

// matchAndAllocate 匹配规则并生成分摊结果
func (s *AllocationService) matchAndAllocate(bill costdomain.UnifiedBill, rule costdomain.AllocationRule, period string, now int64) []costdomain.CostAllocation {
	switch rule.RuleType {
	case "dimension_combo":
		return s.allocateByDimensionCombo(bill, rule, period, now)
	case "tag_mapping":
		return s.allocateByTagMapping(bill, rule, period, now)
	case "shared_ratio":
		return s.allocateBySharedRatio(bill, rule, period, now)
	default:
		return nil
	}
}

// allocateByDimensionCombo 按维度组合分摊
func (s *AllocationService) allocateByDimensionCombo(bill costdomain.UnifiedBill, rule costdomain.AllocationRule, period string, now int64) []costdomain.CostAllocation {
	// Check if any combo matches the bill
	anyMatch := false
	for _, combo := range rule.DimensionCombos {
		if s.comboDimensionsMatch(bill, combo) {
			anyMatch = true
			break
		}
	}
	if !anyMatch {
		return nil
	}

	var allocs []costdomain.CostAllocation
	for _, combo := range rule.DimensionCombos {
		amount := bill.AmountCNY * combo.Ratio / 100.0
		allocs = append(allocs, costdomain.CostAllocation{
			DimType:     rule.DimensionCombos[0].Dimensions[0].DimType,
			DimValue:    combo.TargetID,
			Period:      period,
			TotalAmount: amount,
			RatioAmount: amount,
			RuleID:      rule.ID,
			TenantID:    bill.TenantID,
			CreateTime:  now,
		})
	}
	return allocs
}

// comboDimensionsMatch 检查维度组合是否匹配账单
func (s *AllocationService) comboDimensionsMatch(bill costdomain.UnifiedBill, combo costdomain.DimensionCombo) bool {
	for _, dim := range combo.Dimensions {
		if !s.dimensionMatches(bill, dim) {
			return false
		}
	}
	return true
}

// dimensionMatches 检查单个维度是否匹配账单
func (s *AllocationService) dimensionMatches(bill costdomain.UnifiedBill, dim costdomain.DimensionFilter) bool {
	switch dim.DimType {
	case costdomain.DimCloudAccount:
		return strconv.FormatInt(bill.AccountID, 10) == dim.DimValue
	case costdomain.DimRegion:
		return bill.Region == dim.DimValue
	case costdomain.DimServiceType:
		return bill.ServiceType == dim.DimValue
	case costdomain.DimTag:
		// Tag dimension: DimValue format is "key=value"
		parts := strings.SplitN(dim.DimValue, "=", 2)
		if len(parts) == 2 && bill.Tags != nil {
			return bill.Tags[parts[0]] == parts[1]
		}
		return false
	default:
		return false
	}
}

// allocateByTagMapping 按标签映射分摊
func (s *AllocationService) allocateByTagMapping(bill costdomain.UnifiedBill, rule costdomain.AllocationRule, period string, now int64) []costdomain.CostAllocation {
	if bill.Tags == nil {
		return nil
	}
	tagValue, ok := bill.Tags[rule.TagKey]
	if !ok {
		return nil
	}
	nodeID, ok := rule.TagValueMap[tagValue]
	if !ok {
		return nil
	}

	return []costdomain.CostAllocation{
		{
			DimType:      costdomain.DimTag,
			DimValue:     fmt.Sprintf("%s=%s", rule.TagKey, tagValue),
			NodeID:       nodeID,
			Period:       period,
			TotalAmount:  bill.AmountCNY,
			DirectAmount: bill.AmountCNY,
			RuleID:       rule.ID,
			TenantID:     bill.TenantID,
			CreateTime:   now,
		},
	}
}

// allocateBySharedRatio 按共享资源比例分摊
func (s *AllocationService) allocateBySharedRatio(bill costdomain.UnifiedBill, rule costdomain.AllocationRule, period string, now int64) []costdomain.CostAllocation {
	if rule.SharedConfig == nil {
		return nil
	}

	// Check if this bill's resource is in the shared resource list
	isShared := false
	for _, rid := range rule.SharedConfig.ResourceIDs {
		if bill.ResourceID == rid {
			isShared = true
			break
		}
	}
	if !isShared {
		return nil
	}

	var allocs []costdomain.CostAllocation
	for nodeID, ratio := range rule.SharedConfig.Ratios {
		amount := bill.AmountCNY * ratio / 100.0
		allocs = append(allocs, costdomain.CostAllocation{
			DimType:      "shared",
			NodeID:       nodeID,
			Period:       period,
			TotalAmount:  amount,
			SharedAmount: amount,
			RuleID:       rule.ID,
			TenantID:     bill.TenantID,
			CreateTime:   now,
		})
	}
	return allocs
}

// createUnmatchedAllocation 创建未匹配的分摊记录
func (s *AllocationService) createUnmatchedAllocation(bill costdomain.UnifiedBill, period string, now int64, hasDefault bool, policy costdomain.DefaultAllocationPolicy) costdomain.CostAllocation {
	alloc := costdomain.CostAllocation{
		Period:      period,
		TotalAmount: bill.AmountCNY,
		TenantID:    bill.TenantID,
		CreateTime:  now,
	}

	if hasDefault {
		alloc.DimValue = policy.TargetID
		alloc.DefaultFlag = true
	} else {
		alloc.DimValue = "unallocated"
		alloc.UnallocatedFlag = true
	}

	return alloc
}

// buildTree 构建分摊树形结构
func (s *AllocationService) buildTree(allocs []costdomain.CostAllocation, rootID, dimType string) *AllocationTreeNode {
	// Build a map of node allocations
	nodeMap := make(map[string]*AllocationTreeNode)
	for _, alloc := range allocs {
		nodeID := alloc.DimValue
		if nodeID == "" {
			nodeID = "unallocated"
		}
		if node, ok := nodeMap[nodeID]; ok {
			node.TotalAmount += alloc.TotalAmount
		} else {
			nodeMap[nodeID] = &AllocationTreeNode{
				NodeID:      nodeID,
				NodeName:    nodeID,
				DimType:     dimType,
				TotalAmount: alloc.TotalAmount,
			}
		}
	}

	// Build parent-child relationships from DimPath
	for _, alloc := range allocs {
		if alloc.DimPath == "" {
			continue
		}
		parts := strings.Split(alloc.DimPath, "/")
		for i := 0; i < len(parts)-1; i++ {
			parentID := parts[i]
			childID := parts[i+1]
			parent, pOk := nodeMap[parentID]
			child, cOk := nodeMap[childID]
			if pOk && cOk {
				found := false
				for _, c := range parent.Children {
					if c.NodeID == childID {
						found = true
						break
					}
				}
				if !found {
					parent.Children = append(parent.Children, child)
				}
			}
		}
	}

	if root, ok := nodeMap[rootID]; ok {
		return root
	}

	// Return a synthetic root with all nodes as children
	root := &AllocationTreeNode{
		NodeID:   rootID,
		NodeName: "全部",
		DimType:  dimType,
	}
	for _, node := range nodeMap {
		root.TotalAmount += node.TotalAmount
		root.Children = append(root.Children, node)
	}
	return root
}

// buildTreeFromBills 当没有分摊结果时，直接从统一账单按维度聚合构建树
func (s *AllocationService) buildTreeFromBills(ctx context.Context, tenantID, dimType, rootID, period string) (*AllocationTreeNode, error) {
	startDate := period + "-01"
	endDate := s.periodEndDate(period)

	// 标签维度需要特殊处理：展开 tags map 再聚合
	if dimType == "tag" {
		return s.buildTreeFromTags(ctx, tenantID, rootID, startDate, endDate)
	}

	field := s.dimTypeToField(dimType)
	if field == "" {
		field = "provider"
	}

	results, err := s.billDAO.AggregateByField(ctx, tenantID, field, startDate, endDate, repository.UnifiedBillFilter{})
	if err != nil {
		return nil, fmt.Errorf("aggregate bills by field: %w", err)
	}

	root := &AllocationTreeNode{
		NodeID:   rootID,
		NodeName: "全部",
		DimType:  dimType,
	}

	for _, r := range results {
		name := r.Key
		if name == "" {
			name = "未分类"
		}
		child := &AllocationTreeNode{
			NodeID:      name,
			NodeName:    name,
			DimType:     dimType,
			TotalAmount: r.AmountCNY,
		}
		root.TotalAmount += r.AmountCNY
		root.Children = append(root.Children, child)
	}

	return root, nil
}

// buildTreeFromTags 按标签维度聚合：展开 tags map，按 tag value 分组
func (s *AllocationService) buildTreeFromTags(ctx context.Context, tenantID, rootID, startDate, endDate string) (*AllocationTreeNode, error) {
	results, err := s.billDAO.AggregateByTag(ctx, tenantID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("aggregate bills by tag: %w", err)
	}

	root := &AllocationTreeNode{
		NodeID:   rootID,
		NodeName: "全部",
		DimType:  "tag",
	}

	for _, r := range results {
		name := r.Key
		if name == "" {
			name = "未标记"
		}
		child := &AllocationTreeNode{
			NodeID:      name,
			NodeName:    name,
			DimType:     "tag",
			TotalAmount: r.AmountCNY,
		}
		root.TotalAmount += r.AmountCNY
		root.Children = append(root.Children, child)
	}

	return root, nil
}

// dimTypeToField 将维度类型映射到统一账单的字段名
func (s *AllocationService) dimTypeToField(dimType string) string {
	switch dimType {
	case "provider":
		return "provider"
	case "cloud_account":
		return "account_name"
	case "product_category":
		return "service_type_name"
	case "region":
		return "region"
	case "service_type":
		return "service_type"
	case "project":
		return "resource_name"
	case "department":
		return "provider"
	default:
		return "provider"
	}
}

// periodEndDate 计算账期的结束日期
func (s *AllocationService) periodEndDate(period string) string {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return period + "-31"
	}
	lastDay := t.AddDate(0, 1, -1)
	return lastDay.Format("2006-01-02")
}
