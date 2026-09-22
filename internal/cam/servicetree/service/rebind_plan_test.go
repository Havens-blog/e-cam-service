package service

import (
	"context"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
)

// 改绑计划（PreviewRebind / ApplyRebind）单测。
// 语义：rule 绑定可被更高优先级规则改绑；manual 永锁；未命中更优规则的保持现状。
// 注意：stubBindingRepo.List 返回多个绑定时用 GetByResource 排序无关，只按 resource 索引。

func rebindTestRules() []stdomain.BindingRule {
	return []stdomain.BindingRule{
		{ID: 101, NodeID: 20, EnvID: 200, Priority: 1, Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "cpp"}}},
		{ID: 102, NodeID: 10, EnvID: 100, Priority: 2, Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "smt"}}},
	}
}

func rebindInstance(id int64, name string) camdomain.Instance {
	return camdomain.Instance{
		ID:        id,
		AssetID:   "i-" + name,
		AssetName: name,
		Attributes: map[string]any{
			"provider": "aliyun",
			"region":   "cn-hangzhou",
		},
	}
}

// TestPreviewRebindReprioritizedRule 更高优先级规则命中 → 生成改绑候选
func TestPreviewRebindReprioritizedRule(t *testing.T) {
	// Arrange
	ruleRepo := &stubRuleRepo{listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
		return rebindTestRules(), nil
	}}
	instanceRepo := &stubInstanceRepo{listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
		return []camdomain.Instance{rebindInstance(1, "嘉立创-生产-CPP-SMT切图服务")}, nil
	}}
	bindingRepo := &stubBindingRepo{listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
		// 现绑到 smt 规则 (node 10, rule 102)
		return []stdomain.ResourceBinding{{ID: 9001, ResourceID: 1, NodeID: 10, EnvID: 100, BindType: stdomain.BindTypeRule, RuleID: 102}}, nil
	}}
	nodeRepo := &stubNodeRepo{getByIDsFn: func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
		return []stdomain.ServiceTreeNode{{ID: 10, Name: "SMT"}, {ID: 20, Name: "CPP"}}, nil
	}}
	s := newTestRuleEngine(ruleRepo, nodeRepo, bindingRepo, instanceRepo, nil)

	// Act
	plan, err := s.PreviewRebind(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 1 || len(plan.Items) != 1 {
		t.Fatalf("plan total = %d, items = %d; want 1/1（cpp 规则优先级更高，应改绑）", plan.Total, len(plan.Items))
	}
	c := plan.Items[0]
	if c.ResourceID != 1 || c.ToNodeID != 20 || c.ToRuleID != 101 {
		t.Errorf("candidate = %+v, want to node 20 / rule 101", c)
	}
	if c.FromNodeID != 10 || c.FromRuleID != 102 {
		t.Errorf("from = %+v, want from node 10 / rule 102", c)
	}
	if c.FromNodeName != "SMT" || c.ToNodeName != "CPP" {
		t.Errorf("node names = %q → %q, want SMT → CPP", c.FromNodeName, c.ToNodeName)
	}
}

// TestPreviewRebindManualLocked manual 绑定永不入候选
func TestPreviewRebindManualLocked(t *testing.T) {
	ruleRepo := &stubRuleRepo{listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
		return rebindTestRules(), nil
	}}
	instanceRepo := &stubInstanceRepo{listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
		return []camdomain.Instance{rebindInstance(1, "嘉立创-生产-CPP-SMT切图服务")}, nil
	}}
	bindingRepo := &stubBindingRepo{listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
		return []stdomain.ResourceBinding{{ID: 9001, ResourceID: 1, NodeID: 10, EnvID: 100, BindType: stdomain.BindTypeManual}}, nil
	}}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	plan, err := s.PreviewRebind(context.Background(), 1)
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 0 {
		t.Errorf("manual 绑定被误纳入改绑候选，total = %d, want 0", plan.Total)
	}
}

// TestPreviewRebindNoChange 现绑定已是最优规则 → 无候选
func TestPreviewRebindNoChange(t *testing.T) {
	ruleRepo := &stubRuleRepo{listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
		return rebindTestRules(), nil
	}}
	instanceRepo := &stubInstanceRepo{listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
		return []camdomain.Instance{rebindInstance(1, "cpp-服务")}, nil
	}}
	bindingRepo := &stubBindingRepo{listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
		// 已绑在 cpp 规则 (node 20, rule 101)，仍是最优
		return []stdomain.ResourceBinding{{ID: 9001, ResourceID: 1, NodeID: 20, EnvID: 200, BindType: stdomain.BindTypeRule, RuleID: 101}}, nil
	}}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	plan, err := s.PreviewRebind(context.Background(), 1)
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 0 {
		t.Errorf("现绑已最优却被判为需改绑，total = %d, want 0", plan.Total)
	}
}

// TestPreviewRebindSkipsUnbound 未绑定资产不入候选（属 ExecuteRules 创建范畴）
func TestPreviewRebindSkipsUnbound(t *testing.T) {
	ruleRepo := &stubRuleRepo{listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
		return rebindTestRules(), nil
	}}
	instanceRepo := &stubInstanceRepo{listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
		return []camdomain.Instance{rebindInstance(1, "cpp-服务")}, nil
	}}
	bindingRepo := &stubBindingRepo{listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
		return nil, nil
	}}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	plan, err := s.PreviewRebind(context.Background(), 1)
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 0 {
		t.Errorf("未绑定资产被误纳入改绑候选，total = %d, want 0", plan.Total)
	}
}

// TestApplyRebindAppliesRequestedAndSkipsOther 只对请求中的候选资源改绑，非候选跳过
func TestApplyRebindAppliesRequestedAndSkipsOther(t *testing.T) {
	ruleRepo := &stubRuleRepo{listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
		return rebindTestRules(), nil
	}}
	instanceRepo := &stubInstanceRepo{listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
		return []camdomain.Instance{
			rebindInstance(1, "cpp-a"),
			rebindInstance(2, "cpp-b"),
		}, nil
	}}
	bindingRepo := &stubBindingRepo{listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
		return []stdomain.ResourceBinding{
			{ID: 9001, ResourceID: 1, NodeID: 10, EnvID: 100, BindType: stdomain.BindTypeRule, RuleID: 102},
			{ID: 9002, ResourceID: 2, NodeID: 10, EnvID: 100, BindType: stdomain.BindTypeRule, RuleID: 102},
		}, nil
	}}
	nodeRepo := &stubNodeRepo{getByIDsFn: func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
		return nil, nil // 节点名回填失败不应影响改绑主流程
	}}
	s := newTestRuleEngine(ruleRepo, nodeRepo, bindingRepo, instanceRepo, nil)

	// Act：只申请改绑资源 1，并混入一个非候选资源 999
	applied, err := s.ApplyRebind(context.Background(), 1, []int64{1, 999})

	if err != nil {
		t.Fatalf("ApplyRebind() error = %v", err)
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1（仅资源 1；999 非候选应跳过）", applied)
	}
	if len(bindingRepo.updateCalls) != 1 {
		t.Fatalf("UpdateTarget 调用次数 = %d, want 1", len(bindingRepo.updateCalls))
	}
	call := bindingRepo.updateCalls[0]
	if call.id != 9001 || call.nodeID != 20 || call.envID != 200 || call.ruleID != 101 {
		t.Errorf("UpdateTarget = %+v, want id=9001 node=20 env=200 rule=101", call)
	}
}