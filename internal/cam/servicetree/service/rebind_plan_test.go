package service

import (
	"context"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
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
	// 环境口径（任务 3）：实例名无环境信号 → 沿用现绑 env 100（而非规则写死的 200），
	// 避免改绑把无信号资产的环境改成规则值
	if call.id != 9001 || call.nodeID != 20 || call.envID != 100 || call.ruleID != 101 {
		t.Errorf("UpdateTarget = %+v, want id=9001 node=20 env=100 rule=101（无信号沿用现绑 env）", call)
	}
}

// rebindEnvTestEngine 组装带环境表的改绑测试环境：单 cpp 规则 + 实例 + 现绑，
// 返回服务与绑定仓储（供断言 UpdateTarget 调用）。
func rebindEnvTestEngine(
	t *testing.T,
	rule stdomain.BindingRule,
	instance camdomain.Instance,
	binding stdomain.ResourceBinding,
	envs []stdomain.Environment,
	withEnvRepo bool,
) (RuleEngineService, *stubBindingRepo) {
	t.Helper()
	ruleRepo := &stubRuleRepo{listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
		return []stdomain.BindingRule{rule}, nil
	}}
	instanceRepo := &stubInstanceRepo{listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
		return []camdomain.Instance{instance}, nil
	}}
	bindingRepo := &stubBindingRepo{listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
		return []stdomain.ResourceBinding{binding}, nil
	}}
	nodeRepo := &stubNodeRepo{getByIDsFn: func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
		return []stdomain.ServiceTreeNode{{ID: 10, Name: "SMT"}, {ID: 20, Name: "CPP"}}, nil
	}}
	var envRepo repository.EnvironmentRepository // 保持 nil interface（非 typed-nil）
	if withEnvRepo {
		envRepo = &inferEnvRepoStub{listFn: func(ctx context.Context, filter stdomain.EnvironmentFilter) ([]stdomain.Environment, error) {
			return envs, nil
		}}
	}
	return newTestRuleEngine(ruleRepo, nodeRepo, bindingRepo, instanceRepo, envRepo), bindingRepo
}

// rebindEnvTestRule 改绑测试用 cpp 规则：node 20 / env 200（规则写死环境）
func rebindEnvTestRule() stdomain.BindingRule {
	return stdomain.BindingRule{
		ID: 101, NodeID: 20, EnvID: 200, Priority: 1, Name: "cpp规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "cpp"}},
	}
}

// TestPreviewRebindEnvOnlyChange 仅环境变化、节点不变也入候选（任务 3 核心）：
// rule 绑定资产的目标环境按资产推断重算，历史「prod 命名绑 dev」资产以此为纠正入口。
func TestPreviewRebindEnvOnlyChange(t *testing.T) {
	tests := []struct {
		name      string
		instance  camdomain.Instance
		wantToEnv int64
	}{
		{
			name:      "命名 -prod- 信号：dev 现绑改判 prod",
			instance:  envTestInstance(1, "rod-cpp-prod-app-01", nil),
			wantToEnv: 9,
		},
		{
			name:      "tag.environment=uat 信号：对齐租户 uat 环境",
			instance:  envTestInstance(1, "rod-cpp-app-01", map[string]any{"environment": "uat"}),
			wantToEnv: 8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange: 资产已按 cpp 规则绑在目标节点 20、规则写死 env 200
			binding := stdomain.ResourceBinding{ID: 9001, ResourceID: 1, NodeID: 20, EnvID: 200, BindType: stdomain.BindTypeRule, RuleID: 101}
			s, _ := rebindEnvTestEngine(t, rebindEnvTestRule(), tt.instance, binding, envTestTenantEnvs(), true)

			// Act
			plan, err := s.PreviewRebind(context.Background(), 1)

			// Assert
			if err != nil {
				t.Fatalf("PreviewRebind() error = %v", err)
			}
			if plan.Total != 1 || len(plan.Items) != 1 {
				t.Fatalf("仅环境变化应入候选: total=%d items=%d, want 1", plan.Total, len(plan.Items))
			}
			c := plan.Items[0]
			if c.FromNodeID != 20 || c.ToNodeID != 20 {
				t.Errorf("节点应保持不变: from=%d to=%d, want 20/20", c.FromNodeID, c.ToNodeID)
			}
			if c.FromEnvID != 200 || c.ToEnvID != tt.wantToEnv {
				t.Errorf("env = %d→%d, want 200→%d（目标环境按推断）", c.FromEnvID, c.ToEnvID, tt.wantToEnv)
			}
			if c.ToRuleID != 101 {
				t.Errorf("ToRuleID = %d, want 101", c.ToRuleID)
			}
		})
	}
}

// TestPreviewRebindNoSignalKeepsCurrentEnv 无信号资产不被环境重算误改：
// 推断未命中沿用现绑 env（而非规则 env），节点也不变 → 不产生候选。
func TestPreviewRebindNoSignalKeepsCurrentEnv(t *testing.T) {
	// Arrange: 规则写死 env 200 与现绑 env 100 不同，但资产名无环境信号
	instance := envTestInstance(1, "rod-cpp-app-01", nil)
	binding := stdomain.ResourceBinding{ID: 9001, ResourceID: 1, NodeID: 20, EnvID: 100, BindType: stdomain.BindTypeRule, RuleID: 101}
	s, _ := rebindEnvTestEngine(t, rebindEnvTestRule(), instance, binding, envTestTenantEnvs(), true)

	// Act
	plan, err := s.PreviewRebind(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 0 {
		t.Errorf("无信号资产被规则 env 差异误判为候选, total = %d, want 0（沿用现绑 env）", plan.Total)
	}
}

// TestPreviewRebindNilEnvRepoDegradesToNodeOnly envRepo 未注入（降级路径）：
// 环境重算退化为现绑兜底，prod 命名资产不再因环境差异入候选，行为同任务 3 之前。
func TestPreviewRebindNilEnvRepoDegradesToNodeOnly(t *testing.T) {
	// Arrange: prod 命名资产绑在规则目标节点 20 上，仅环境与规则写死值不同
	instance := envTestInstance(1, "rod-cpp-prod-app-01", nil)
	binding := stdomain.ResourceBinding{ID: 9001, ResourceID: 1, NodeID: 20, EnvID: 200, BindType: stdomain.BindTypeRule, RuleID: 101}
	s, _ := rebindEnvTestEngine(t, rebindEnvTestRule(), instance, binding, nil, false)

	// Act
	plan, err := s.PreviewRebind(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 0 {
		t.Errorf("降级路径应退化为仅节点口径, total = %d, want 0", plan.Total)
	}
}

// TestPreviewRebindManualLockedWithEnvSignal manual 永锁不变：即便资产带环境信号
// （命名 -prod- 可推断），手动绑定也不入候选。
func TestPreviewRebindManualLockedWithEnvSignal(t *testing.T) {
	// Arrange
	instance := envTestInstance(1, "rod-cpp-prod-app-01", nil)
	binding := stdomain.ResourceBinding{ID: 9001, ResourceID: 1, NodeID: 20, EnvID: 200, BindType: stdomain.BindTypeManual}
	s, _ := rebindEnvTestEngine(t, rebindEnvTestRule(), instance, binding, envTestTenantEnvs(), true)

	// Act
	plan, err := s.PreviewRebind(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("PreviewRebind() error = %v", err)
	}
	if plan.Total != 0 {
		t.Errorf("manual 绑定被环境重算误纳入候选, total = %d, want 0", plan.Total)
	}
}

// TestApplyRebindCorrectsEnv 确认改绑后环境纠正落库：UpdateTarget 收到推断 env
// （prod），而非规则写死的 env 200。
func TestApplyRebindCorrectsEnv(t *testing.T) {
	// Arrange: prod 命名资产现绑规则目标节点 20、env 200（dev 语义）
	instance := envTestInstance(1, "rod-cpp-prod-app-01", nil)
	binding := stdomain.ResourceBinding{ID: 9001, ResourceID: 1, NodeID: 20, EnvID: 200, BindType: stdomain.BindTypeRule, RuleID: 101}
	s, bindingRepo := rebindEnvTestEngine(t, rebindEnvTestRule(), instance, binding, envTestTenantEnvs(), true)

	// Act
	applied, err := s.ApplyRebind(context.Background(), 1, []int64{1})

	// Assert
	if err != nil {
		t.Fatalf("ApplyRebind() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	if len(bindingRepo.updateCalls) != 1 {
		t.Fatalf("UpdateTarget 调用次数 = %d, want 1", len(bindingRepo.updateCalls))
	}
	call := bindingRepo.updateCalls[0]
	if call.id != 9001 || call.nodeID != 20 || call.envID != 9 || call.ruleID != 101 {
		t.Errorf("UpdateTarget = %+v, want id=9001 node=20 env=9 rule=101（环境纠正落库）", call)
	}
}