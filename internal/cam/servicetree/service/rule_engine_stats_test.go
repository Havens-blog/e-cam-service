package service

import (
	"context"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// TestRuleEngineGetFieldValue 锁定 getFieldValue 字段取值语义。
// region 为一期修复点：UI 有「地域」条件选项，后端此前无该 case 永不匹配。
func TestRuleEngineGetFieldValue(t *testing.T) {
	s := &ruleEngineService{}
	tests := []struct {
		name     string
		instance camdomain.Instance
		field    string
		want     string
	}{
		{
			name:     "region 命中 attributes.region",
			instance: camdomain.Instance{Attributes: map[string]any{"region": "cn-hangzhou"}},
			field:    "region",
			want:     "cn-hangzhou",
		},
		{
			name:     "region 缺失返回空串不匹配",
			instance: camdomain.Instance{Attributes: map[string]any{}},
			field:    "region",
			want:     "",
		},
		{
			name:     "region 非字符串返回空串",
			instance: camdomain.Instance{Attributes: map[string]any{"region": 42}},
			field:    "region",
			want:     "",
		},
		{
			name:     "既有字段 name 不受影响",
			instance: camdomain.Instance{AssetName: "web-01"},
			field:    "name",
			want:     "web-01",
		},
		{
			name:     "既有 attributes.xxx 取值不受影响",
			instance: camdomain.Instance{Attributes: map[string]any{"project_id": "pg-1"}},
			field:    "attributes.project_id",
			want:     "pg-1",
		},
		{
			name:     "既有 tag.xxx 取值不受影响",
			instance: camdomain.Instance{Attributes: map[string]any{"tags": map[string]any{"env": "prod"}}},
			field:    "tag.env",
			want:     "prod",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.getFieldValue(tt.instance, tt.field); got != tt.want {
				t.Errorf("getFieldValue(field=%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// ---- 测试桩：嵌入接口，仅实现被调用方法 ----

type statsCall struct {
	id         int64
	executedAt time.Time
	matchCount int64
}

type stubRuleRepo struct {
	repository.RuleRepository
	listEnabledFn func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error)
	listFn        func(ctx context.Context, filter stdomain.RuleFilter) ([]stdomain.BindingRule, error)
	countFn       func(ctx context.Context, filter stdomain.RuleFilter) (int64, error)
	statsCalls    []statsCall
}

func (s *stubRuleRepo) ListEnabled(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
	return s.listEnabledFn(ctx, tenantID)
}

func (s *stubRuleRepo) List(ctx context.Context, filter stdomain.RuleFilter) ([]stdomain.BindingRule, error) {
	return s.listFn(ctx, filter)
}

func (s *stubRuleRepo) Count(ctx context.Context, filter stdomain.RuleFilter) (int64, error) {
	return s.countFn(ctx, filter)
}

func (s *stubRuleRepo) UpdateExecutionStats(ctx context.Context, id int64, executedAt time.Time, matchCount int64) error {
	s.statsCalls = append(s.statsCalls, statsCall{id: id, executedAt: executedAt, matchCount: matchCount})
	return nil
}

type stubNodeRepo struct {
	repository.NodeRepository
	getByIDsCalls [][]int64
	getByIDsFn    func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error)
}

func (s *stubNodeRepo) GetByIDs(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
	s.getByIDsCalls = append(s.getByIDsCalls, ids)
	return s.getByIDsFn(ctx, ids)
}

type stubBindingRepo struct {
	repository.BindingRepository
	listFn          func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error)
	createBatchFn   func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error)
	createBatchHits int
	updateTargetFn  func(ctx context.Context, id int64, nodeID int64, envID int64, ruleID int64) error
	updateCalls     []bindingTargetCall
	countByRuleIDsFn func(ctx context.Context, ruleIDs []int64) (map[int64]int64, error)
	deleteByRuleIDFn func(ctx context.Context, ruleID int64) (int64, error)
}

type bindingTargetCall struct {
	id, nodeID, envID, ruleID int64
}

func (s *stubBindingRepo) List(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
	return s.listFn(ctx, filter)
}

func (s *stubBindingRepo) CreateBatch(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
	s.createBatchHits++
	return s.createBatchFn(ctx, bindings)
}

func (s *stubBindingRepo) UpdateTarget(ctx context.Context, id int64, nodeID int64, envID int64, ruleID int64) error {
	if s.updateTargetFn != nil {
		return s.updateTargetFn(ctx, id, nodeID, envID, ruleID)
	}
	s.updateCalls = append(s.updateCalls, bindingTargetCall{id: id, nodeID: nodeID, envID: envID, ruleID: ruleID})
	return nil
}

func (s *stubBindingRepo) CountByRuleIDs(ctx context.Context, ruleIDs []int64) (map[int64]int64, error) {
	if s.countByRuleIDsFn != nil {
		return s.countByRuleIDsFn(ctx, ruleIDs)
	}
	return map[int64]int64{}, nil
}

func (s *stubBindingRepo) DeleteByRuleID(ctx context.Context, ruleID int64) (int64, error) {
	if s.deleteByRuleIDFn != nil {
		return s.deleteByRuleIDFn(ctx, ruleID)
	}
	return 0, nil
}

type stubInstanceRepo struct {
	camrepo.InstanceRepository
	listFn func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error)
}

func (s *stubInstanceRepo) List(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
	return s.listFn(ctx, filter)
}

func newTestRuleEngine(ruleRepo repository.RuleRepository, nodeRepo repository.NodeRepository, bindingRepo repository.BindingRepository, instanceRepo camrepo.InstanceRepository, envRepo repository.EnvironmentRepository) RuleEngineService {
	return NewRuleEngineService(ruleRepo, bindingRepo, nodeRepo, instanceRepo, envRepo, elog.DefaultLogger)
}

// TestListRulesFillNodeNames 一期修复①：node_name 批量回填，N+1 避免。
func TestListRulesFillNodeNames(t *testing.T) {
	// Arrange
	rules := []stdomain.BindingRule{
		{ID: 1, NodeID: 11, Name: "r1"},
		{ID: 2, NodeID: 11, Name: "r2"}, // 同节点，去重后只查一次
		{ID: 3, NodeID: 99, Name: "r3"}, // 节点不存在，NodeName 保持空
	}
	ruleRepo := &stubRuleRepo{
		listFn: func(ctx context.Context, filter stdomain.RuleFilter) ([]stdomain.BindingRule, error) {
			return rules, nil
		},
		countFn: func(ctx context.Context, filter stdomain.RuleFilter) (int64, error) { return 3, nil },
	}
	nodeRepo := &stubNodeRepo{
		getByIDsFn: func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
			return []stdomain.ServiceTreeNode{{ID: 11, Name: "SMT订单"}}, nil
		},
	}
	s := newTestRuleEngine(ruleRepo, nodeRepo, &stubBindingRepo{}, &stubInstanceRepo{}, nil)

	// Act
	got, total, err := s.ListRules(context.Background(), stdomain.RuleFilter{TenantID: 1})

	// Assert
	if err != nil {
		t.Fatalf("ListRules() error = %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	// 批量查询只发生一次
	if len(nodeRepo.getByIDsCalls) != 1 {
		t.Fatalf("GetByIDs 调用次数 = %d, want 1", len(nodeRepo.getByIDsCalls))
	}
	if got[0].NodeName != "SMT订单" || got[1].NodeName != "SMT订单" {
		t.Errorf("node_name 未回填: %q, %q", got[0].NodeName, got[1].NodeName)
	}
	if got[2].NodeName != "" {
		t.Errorf("不存在节点的 node_name 应为空, got %q", got[2].NodeName)
	}
}

// TestListRulesFillBindingCounts 批量回填规则当前绑定资源数。
func TestListRulesFillBindingCounts(t *testing.T) {
	rules := []stdomain.BindingRule{
		{ID: 1, Name: "r1"},
		{ID: 2, Name: "r2"},
		{ID: 3, Name: "r3"},
	}
	ruleRepo := &stubRuleRepo{
		listFn: func(ctx context.Context, filter stdomain.RuleFilter) ([]stdomain.BindingRule, error) {
			return rules, nil
		},
		countFn: func(ctx context.Context, filter stdomain.RuleFilter) (int64, error) { return 3, nil },
	}
	nodeRepo := &stubNodeRepo{
		getByIDsFn: func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
			return nil, nil
		},
	}
	bindingRepo := &stubBindingRepo{
		countByRuleIDsFn: func(ctx context.Context, ruleIDs []int64) (map[int64]int64, error) {
			// r1 绑定 5 个、r3 绑定 2 个、r2 无绑定不在返回里
			return map[int64]int64{1: 5, 3: 2}, nil
		},
	}
	s := newTestRuleEngine(ruleRepo, nodeRepo, bindingRepo, &stubInstanceRepo{}, nil)

	got, _, err := s.ListRules(context.Background(), stdomain.RuleFilter{TenantID: 1})
	if err != nil {
		t.Fatalf("ListRules() error = %v", err)
	}
	if got[0].BindingCount != 5 || got[1].BindingCount != 0 || got[2].BindingCount != 2 {
		t.Errorf("binding_count 回填错误: %d, %d, %d, want 5, 0, 2",
			got[0].BindingCount, got[1].BindingCount, got[2].BindingCount)
	}
}

// TestUnbindRuleResources 解绑规则名下所有绑定但保留规则，返回解绑条数。
func TestUnbindRuleResources(t *testing.T) {
	bindingRepo := &stubBindingRepo{
		deleteByRuleIDFn: func(ctx context.Context, ruleID int64) (int64, error) {
			if ruleID != 1 {
				t.Errorf("ruleID = %d, want 1", ruleID)
			}
			return 7, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, bindingRepo, &stubInstanceRepo{}, nil)

	count, err := s.UnbindRuleResources(context.Background(), 1)
	if err != nil {
		t.Fatalf("UnbindRuleResources() error = %v", err)
	}
	if count != 7 {
		t.Errorf("count = %d, want 7", count)
	}
}

// TestListFieldValues 条件字段去重枚举：region/model_uid/tag.* 去重排序，
// name/asset_id 等非枚举字段返回空。
func TestListFieldValues(t *testing.T) {
	instances := []camdomain.Instance{
		{ID: 1, ModelUID: "aliyun_ecs", Attributes: map[string]any{"region": "cn-hangzhou", "tags": map[string]any{"environment": "prod"}}},
		{ID: 2, ModelUID: "aliyun_rds", Attributes: map[string]any{"region": "cn-beijing", "tags": map[string]any{"environment": "dev"}}},
		{ID: 3, ModelUID: "aliyun_ecs", Attributes: map[string]any{"region": "cn-hangzhou", "tags": map[string]any{"environment": "prod"}}}, // 重复
		{ID: 4, ModelUID: "huawei_ecs", Attributes: map[string]any{"region": "cn-south"}},
	}
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return instances, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, &stubBindingRepo{}, instanceRepo, nil)

	assertEqual := func(t *testing.T, field string, want []string) {
		t.Helper()
		got, err := s.ListFieldValues(context.Background(), 1, field)
		if err != nil {
			t.Fatalf("ListFieldValues(%q) error = %v", field, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s len = %d, want %d (got %v)", field, len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s got[%d] = %q, want %q (got %v)", field, i, got[i], want[i], got)
			}
		}
	}

	assertEqual(t, "region", []string{"cn-beijing", "cn-hangzhou", "cn-south"})
	assertEqual(t, "model_uid", []string{"aliyun_ecs", "aliyun_rds", "huawei_ecs"})
	assertEqual(t, "tag.environment", []string{"dev", "prod"})

	// 非枚举字段：直接返回空，不扫描
	for _, field := range []string{"name", "asset_id"} {
		got, _ := s.ListFieldValues(context.Background(), 1, field)
		if len(got) != 0 {
			t.Errorf("%s 应返回空, got %v", field, got)
		}
	}
}

// TestExecuteRulesStatsPersistence 一期修复③：执行结果（匹配数/执行时间）规则级落库。
func TestExecuteRulesStatsPersistence(t *testing.T) {
	rule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "web规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	}
	matchInstance := camdomain.Instance{ID: 7, AssetName: "web-01", Attributes: map[string]any{"region": "cn-hangzhou"}}
	noMatchInstance := camdomain.Instance{ID: 8, AssetName: "db-01"}

	tests := []struct {
		name             string
		instances        []camdomain.Instance
		existingBindings []stdomain.ResourceBinding
		wantNewBindings  int64
		wantStatsCall    bool
		wantMatchCount   int64
		wantCreateBatch  bool
	}{
		{
			name:           "无实例数据也落库（匹配数 0）",
			instances:      nil,
			wantStatsCall:  true,
			wantMatchCount: 0,
		},
		{
			name:            "命中 1 台，匹配数落库为 1",
			instances:       []camdomain.Instance{matchInstance, noMatchInstance},
			wantNewBindings: 1,
			wantStatsCall:   true,
			wantMatchCount:  1,
			wantCreateBatch: true,
		},
		{
			name:      "已在目标环境绑定的实例不计入匹配数",
			instances: []camdomain.Instance{matchInstance},
			existingBindings: []stdomain.ResourceBinding{
				{EnvID: 5, ResourceID: 7, ResourceType: stdomain.ResourceTypeInstance},
			},
			wantStatsCall:  true,
			wantMatchCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			ruleRepo := &stubRuleRepo{
				listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
					return []stdomain.BindingRule{rule}, nil
				},
			}
			bindingRepo := &stubBindingRepo{
				listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
					return tt.existingBindings, nil
				},
				createBatchFn: func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
					return int64(len(bindings)), nil
				},
			}
			instanceRepo := &stubInstanceRepo{
				listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
					return tt.instances, nil
				},
			}
			s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

			// Act
			count, err := s.ExecuteRules(context.Background(), 1)

			// Assert
			if err != nil {
				t.Fatalf("ExecuteRules() error = %v", err)
			}
			if count != tt.wantNewBindings {
				t.Errorf("new bindings = %d, want %d", count, tt.wantNewBindings)
			}
			if (bindingRepo.createBatchHits > 0) != tt.wantCreateBatch {
				t.Errorf("CreateBatch 调用 = %d, wantCreateBatch %v", bindingRepo.createBatchHits, tt.wantCreateBatch)
			}
			if !tt.wantStatsCall {
				if len(ruleRepo.statsCalls) != 0 {
					t.Errorf("不应落库统计, got %v", ruleRepo.statsCalls)
				}
				return
			}
			if len(ruleRepo.statsCalls) != 1 {
				t.Fatalf("统计落库次数 = %d, want 1", len(ruleRepo.statsCalls))
			}
			call := ruleRepo.statsCalls[0]
			if call.id != rule.ID {
				t.Errorf("统计规则ID = %d, want %d", call.id, rule.ID)
			}
			if call.matchCount != tt.wantMatchCount {
				t.Errorf("last_match_count = %d, want %d", call.matchCount, tt.wantMatchCount)
			}
			if call.executedAt.IsZero() {
				t.Error("last_executed_at 不应为零值")
			}
		})
	}
}
