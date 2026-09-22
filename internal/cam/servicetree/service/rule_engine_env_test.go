package service

import (
	"context"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
)

// ExecuteRules 绑定环境落库单测（推断优先、规则 env 兜底）。
// 背景：原实现恒用 rule.EnvID，跨环境资产（如 rod-cpp-prod-*）被统一绑到规则的 dev
// 环境，是 691 条环境错标的根因之一；本组测试锁定「资产推断优先、规则 env 兜底」。

// envTestTenantEnvs 租户环境表：dev=5 / uat=8 / prod=9
func envTestTenantEnvs() []stdomain.Environment {
	return []stdomain.Environment{
		{ID: 5, Code: "dev"},
		{ID: 8, Code: "uat"},
		{ID: 9, Code: "prod"},
	}
}

// envTestInstance 构造带可选 tags 的资产实例
func envTestInstance(id int64, name string, tags map[string]any) camdomain.Instance {
	attrs := map[string]any{}
	if tags != nil {
		attrs["tags"] = tags
	}
	return camdomain.Instance{ID: id, AssetName: name, Attributes: attrs}
}

// envTestEngine 组装 ExecuteRules 测试环境：单规则（contains rod-cpp）+ 单实例，
// 返回服务与落库捕获切片。
func envTestEngine(
	t *testing.T,
	rules []stdomain.BindingRule,
	instances []camdomain.Instance,
	existingBindings []stdomain.ResourceBinding,
	envs []stdomain.Environment,
	withEnvRepo bool,
) (RuleEngineService, *[]stdomain.ResourceBinding, *stubBindingRepo) {
	t.Helper()

	ruleRepo := &stubRuleRepo{
		listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
			return rules, nil
		},
	}
	var created []stdomain.ResourceBinding
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return existingBindings, nil
		},
		createBatchFn: func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
			created = append(created, bindings...)
			return int64(len(bindings)), nil
		},
	}
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return instances, nil
		},
	}
	var envRepo repository.EnvironmentRepository // 保持 nil interface（非 typed-nil），触发服务端降级路径
	if withEnvRepo {
		envRepo = &inferEnvRepoStub{
			listFn: func(ctx context.Context, filter stdomain.EnvironmentFilter) ([]stdomain.Environment, error) {
				return envs, nil
			},
		}
	}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, envRepo)
	return s, &created, bindingRepo
}

// TestExecuteRulesBindsInferredEnv 落库 EnvID：推断命中 → 资产环境码对应 env_id；
// 未命中（无信号/非标准码/码未映射租户环境）→ 规则 env 兜底。节点仍按规则。
func TestExecuteRulesBindsInferredEnv(t *testing.T) {
	cppRule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "cpp规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "rod-cpp"}},
	}
	tests := []struct {
		name      string
		envs      []stdomain.Environment
		instance  camdomain.Instance
		wantEnvID int64
	}{
		{name: "命名 -prod- 命中落 prod 环境", instance: envTestInstance(7, "rod-cpp-prod-app-01", nil), wantEnvID: 9},
		{name: "命名 -uat- 归一落租户 uat 环境", instance: envTestInstance(7, "rod-cpp-uat-web-01", nil), wantEnvID: 8},
		{name: "推断码 test 无租户环境走兜底", instance: envTestInstance(7, "rod-cpp-test-web-01", nil), wantEnvID: 5},
		{name: "tag.environment=prod 命中", instance: envTestInstance(7, "rod-cpp-app-01", map[string]any{"environment": "prod"}), wantEnvID: 9},
		{name: "无信号走规则 env 兜底", instance: envTestInstance(7, "rod-cpp-app-01", nil), wantEnvID: 5},
		{name: "命名 fat 非标准码走兜底", instance: envTestInstance(7, "rod-cpp-fat-app-01", nil), wantEnvID: 5},
		{name: "tag fat 非标准码走兜底", instance: envTestInstance(7, "rod-cpp-app-01", map[string]any{"environment": "fat"}), wantEnvID: 5},
		{
			name:      "推断码未映射租户环境走兜底",
			envs:      []stdomain.Environment{{ID: 5, Code: "dev"}},
			instance:  envTestInstance(7, "rod-cpp-prod-app-01", nil),
			wantEnvID: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			envs := tt.envs
			if envs == nil {
				envs = envTestTenantEnvs()
			}
			s, created, _ := envTestEngine(t,
				[]stdomain.BindingRule{cppRule},
				[]camdomain.Instance{tt.instance},
				nil, envs, true)

			// Act
			count, err := s.ExecuteRules(context.Background(), 1)

			// Assert
			if err != nil {
				t.Fatalf("ExecuteRules() error = %v", err)
			}
			if count != 1 || len(*created) != 1 {
				t.Fatalf("新增绑定 = %d (created %d), want 1", count, len(*created))
			}
			got := (*created)[0]
			if got.EnvID != tt.wantEnvID {
				t.Errorf("落库 EnvID = %d, want %d（推断优先、规则兜底）", got.EnvID, tt.wantEnvID)
			}
			if got.NodeID != cppRule.NodeID {
				t.Errorf("落库 NodeID = %d, want %d（节点仍按规则）", got.NodeID, cppRule.NodeID)
			}
			if got.RuleID != cppRule.ID {
				t.Errorf("落库 RuleID = %d, want %d", got.RuleID, cppRule.ID)
			}
		})
	}
}

// TestExecuteRulesSkipsBoundResources create-only 幂等 + manual 锁定不变：
// 已绑定资产（manual 或 rule，任意环境）整体跳过，推断不参与、规则不覆盖。
func TestExecuteRulesSkipsBoundResources(t *testing.T) {
	cppRule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "cpp规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "rod-cpp"}},
	}
	tests := []struct {
		name     string
		bindType string
	}{
		{name: "manual 绑定不被规则覆盖", bindType: stdomain.BindTypeManual},
		{name: "rule 绑定不重复落库", bindType: stdomain.BindTypeRule},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange: 已绑定到任意环境（含与推断结果不同的环境），推断命中也不改绑
			existing := []stdomain.ResourceBinding{
				{EnvID: 5, ResourceID: 7, ResourceType: stdomain.ResourceTypeInstance, BindType: tt.bindType, RuleID: 99},
			}
			s, created, bindingRepo := envTestEngine(t,
				[]stdomain.BindingRule{cppRule},
				[]camdomain.Instance{envTestInstance(7, "rod-cpp-prod-app-01", nil)},
				existing, envTestTenantEnvs(), true)

			// Act
			count, err := s.ExecuteRules(context.Background(), 1)

			// Assert
			if err != nil {
				t.Fatalf("ExecuteRules() error = %v", err)
			}
			if count != 0 || len(*created) != 0 {
				t.Errorf("已绑定资产应整体跳过: count=%d created=%d, want 0", count, len(*created))
			}
			if bindingRepo.createBatchHits != 0 {
				t.Errorf("CreateBatch 调用 %d 次, want 0（零重复绑定）", bindingRepo.createBatchHits)
			}
		})
	}
}

// TestExecuteRulesRerunZeroDuplicateBindings 重复执行零重复绑定：首轮落库后，
// 二轮以首轮结果为已绑定集合，命中资产全部跳过。
func TestExecuteRulesRerunZeroDuplicateBindings(t *testing.T) {
	// Arrange
	cppRule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "cpp规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "rod-cpp"}},
	}
	ruleRepo := &stubRuleRepo{
		listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
			return []stdomain.BindingRule{cppRule}, nil
		},
	}
	current := make([]stdomain.ResourceBinding, 0, 1) // 模拟绑定表：首轮落库后累积
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return current, nil
		},
		createBatchFn: func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
			current = append(current, bindings...)
			return int64(len(bindings)), nil
		},
	}
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return []camdomain.Instance{envTestInstance(7, "rod-cpp-prod-app-01", nil)}, nil
		},
	}
	envRepo := &inferEnvRepoStub{
		listFn: func(ctx context.Context, filter stdomain.EnvironmentFilter) ([]stdomain.Environment, error) {
			return envTestTenantEnvs(), nil
		},
	}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, envRepo)
	ctx := context.Background()

	// Act: 首轮落 1 条，二轮应零新增
	first, err := s.ExecuteRules(ctx, 1)
	if err != nil {
		t.Fatalf("首轮 ExecuteRules() error = %v", err)
	}
	second, err := s.ExecuteRules(ctx, 1)

	// Assert
	if err != nil {
		t.Fatalf("二轮 ExecuteRules() error = %v", err)
	}
	if first != 1 {
		t.Errorf("首轮新增 = %d, want 1", first)
	}
	if second != 0 {
		t.Errorf("二轮新增 = %d, want 0（create-only 幂等，零重复绑定）", second)
	}
	if len(current) != 1 {
		t.Errorf("绑定表共 %d 条, want 1", len(current))
	}
	if current[0].EnvID != 9 {
		t.Errorf("首轮落库 EnvID = %d, want 9（prod 推断仍生效）", current[0].EnvID)
	}
}

// TestExecuteRulesFirstMatchKeepsNodeEnvCorrected 优先级语义不变：仍取首个命中规则
// （节点/规则按首个命中，仅环境被资产推断纠正）。
func TestExecuteRulesFirstMatchKeepsNodeEnvCorrected(t *testing.T) {
	// Arrange: 两条规则同条件，首个命中优先级高
	ruleA := stdomain.BindingRule{
		ID: 1, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "高优规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "rod-cpp"}},
	}
	ruleB := stdomain.BindingRule{
		ID: 2, NodeID: 22, EnvID: 8, TenantID: 1, Enabled: true, Name: "低优规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "rod-cpp"}},
	}
	s, created, _ := envTestEngine(t,
		[]stdomain.BindingRule{ruleA, ruleB},
		[]camdomain.Instance{envTestInstance(7, "rod-cpp-prod-app-01", nil)},
		nil, envTestTenantEnvs(), true)

	// Act
	count, err := s.ExecuteRules(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("ExecuteRules() error = %v", err)
	}
	if count != 1 || len(*created) != 1 {
		t.Fatalf("新增绑定 = %d (created %d), want 1", count, len(*created))
	}
	got := (*created)[0]
	if got.RuleID != ruleA.ID || got.NodeID != ruleA.NodeID {
		t.Errorf("应取首个命中规则: rule=%d node=%d, want rule=%d node=%d", got.RuleID, got.NodeID, ruleA.ID, ruleA.NodeID)
	}
	if got.EnvID != 9 {
		t.Errorf("环境仍被资产纠正 = %d, want 9", got.EnvID)
	}
}

// TestExecuteRulesNilEnvRepoFallsBack envRepo 未注入（降级路径）时不 panic，
// 绑定退回规则 env 兜底，行为同现状。
func TestExecuteRulesNilEnvRepoFallsBack(t *testing.T) {
	// Arrange
	cppRule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "cpp规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "rod-cpp"}},
	}
	s, created, _ := envTestEngine(t,
		[]stdomain.BindingRule{cppRule},
		[]camdomain.Instance{envTestInstance(7, "rod-cpp-prod-app-01", nil)},
		nil, nil, false)

	// Act
	count, err := s.ExecuteRules(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("ExecuteRules() error = %v", err)
	}
	if count != 1 || len(*created) != 1 {
		t.Fatalf("新增绑定 = %d (created %d), want 1", count, len(*created))
	}
	if (*created)[0].EnvID != 5 {
		t.Errorf("降级路径 EnvID = %d, want 5（规则 env 兜底）", (*created)[0].EnvID)
	}
}

// TestResolveEnvID 环境解析纯函数：推断码命中映射 → 对应 env_id；
// 空码/未映射码 → 规则 env 兜底。
func TestResolveEnvID(t *testing.T) {
	// Arrange
	m := map[string]int64{"dev": 5, "staging": 8, "prod": 9}
	tests := []struct {
		name    string
		code    string
		ruleEnv int64
		want    int64
	}{
		{name: "命中 prod 取映射 env", code: "prod", ruleEnv: 5, want: 9},
		{name: "staging 归一键命中", code: "staging", ruleEnv: 5, want: 8},
		{name: "空码兜底规则 env", code: "", ruleEnv: 5, want: 5},
		{name: "未映射码兜底规则 env", code: "fat", ruleEnv: 5, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := resolveEnvID(tt.code, tt.ruleEnv, m)
			// Assert
			if got != tt.want {
				t.Errorf("resolveEnvID(%q, %d) = %d, want %d", tt.code, tt.ruleEnv, got, tt.want)
			}
		})
	}
}
