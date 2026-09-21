package service

import (
	"context"
	"errors"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
)

// DryRunRules 单测：命中/不命中/正则条件/上限保护/当前绑定状态标注/只读不落库。
// 复用 rule_engine_stats_test.go 中的测试桩（同包）。

func dryRunTestInstances() []camdomain.Instance {
	return []camdomain.Instance{
		{ID: 7, AssetID: "i-web-01", AssetName: "web-01", Attributes: map[string]any{"provider": "aliyun", "region": "cn-hangzhou"}},
		{ID: 8, AssetID: "i-db-01", AssetName: "db-01", Attributes: map[string]any{"provider": "aws", "region": "us-east-1"}},
	}
}

// TestDryRunRules 基础命中/不命中/正则语义（复用 matchRule/matchCondition，不复制匹配实现）
func TestDryRunRules(t *testing.T) {
	tests := []struct {
		name       string
		conditions []stdomain.RuleCondition
		wantTotal  int64
		wantAssets []string // 期望命中 asset_id 序列
	}{
		{
			name:       "contains 条件命中 1 台",
			conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
			wantTotal:  1,
			wantAssets: []string{"i-web-01"},
		},
		{
			name:       "不命中任何资产",
			conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "k8s-node"}},
			wantTotal:  0,
			wantAssets: nil,
		},
		{
			name:       "regex 条件命中",
			conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorRegex, Value: `^web-[0-9]+$`}},
			wantTotal:  1,
			wantAssets: []string{"i-web-01"},
		},
		{
			name:       "regex 条件不命中（大小写敏感）",
			conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorRegex, Value: `^Web`}},
			wantTotal:  0,
			wantAssets: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			instanceRepo := &stubInstanceRepo{
				listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
					return dryRunTestInstances(), nil
				},
			}
			bindingRepo := &stubBindingRepo{
				listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
					return nil, nil
				},
			}
			s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, bindingRepo, instanceRepo)

			// Act
			result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{Conditions: tt.conditions})

			// Assert
			if err != nil {
				t.Fatalf("DryRunRules() error = %v", err)
			}
			if result.Total != tt.wantTotal {
				t.Errorf("total = %d, want %d", result.Total, tt.wantTotal)
			}
			if len(result.Items) != len(tt.wantAssets) {
				t.Fatalf("items len = %d, want %d", len(result.Items), len(tt.wantAssets))
			}
			for i, wantAsset := range tt.wantAssets {
				if result.Items[i].AssetID != wantAsset {
					t.Errorf("items[%d].asset_id = %q, want %q", i, result.Items[i].AssetID, wantAsset)
				}
			}
		})
	}
}

// TestDryRunRulesHitItemFields 命中项携带 provider/region（与 node_asset.go 取值口径一致）与未绑定状态
func TestDryRunRulesHitItemFields(t *testing.T) {
	// Arrange
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return dryRunTestInstances(), nil
		},
	}
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return nil, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, bindingRepo, instanceRepo)

	// Act
	result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	})

	// Assert
	if err != nil {
		t.Fatalf("DryRunRules() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(result.Items))
	}
	item := result.Items[0]
	if item.ResourceID != 7 || item.AssetID != "i-web-01" || item.AssetName != "web-01" {
		t.Errorf("item 标识字段不符: %+v", item)
	}
	if item.Provider != "aliyun" || item.Region != "cn-hangzhou" {
		t.Errorf("provider/region = %q/%q, want aliyun/cn-hangzhou", item.Provider, item.Region)
	}
	if item.BindStatus != stdomain.BindStatusUnbound {
		t.Errorf("bind_status = %q, want %q", item.BindStatus, stdomain.BindStatusUnbound)
	}
}

// TestDryRunRulesEmptyConditions 条件为空直接拒绝（handler 层映射 400）
func TestDryRunRulesEmptyConditions(t *testing.T) {
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, &stubBindingRepo{}, &stubInstanceRepo{})

	_, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{Conditions: nil})
	if !errors.Is(err, stdomain.ErrRuleConditionsEmpty) {
		t.Errorf("err = %v, want ErrRuleConditionsEmpty", err)
	}
}

// TestDryRunRulesBindingStatus 当前绑定状态标注：手动/规则绑定如实标注，指定环境时按环境口径判定
func TestDryRunRulesBindingStatus(t *testing.T) {
	bindings := []stdomain.ResourceBinding{
		{EnvID: 5, ResourceID: 7, ResourceType: stdomain.ResourceTypeInstance, BindType: stdomain.BindTypeManual, NodeID: 11},
	}
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return dryRunTestInstances(), nil
		},
	}
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return bindings, nil
		},
	}
	nodeRepo := &stubNodeRepo{
		getByIDsFn: func(ctx context.Context, ids []int64) ([]stdomain.ServiceTreeNode, error) {
			return []stdomain.ServiceTreeNode{{ID: 11, Name: "SMT订单"}}, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, nodeRepo, bindingRepo, instanceRepo)
	conds := []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}}

	t.Run("不限环境：标注手动绑定节点并回填节点名", func(t *testing.T) {
		result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{Conditions: conds})
		if err != nil {
			t.Fatalf("DryRunRules() error = %v", err)
		}
		if len(result.Items) != 1 {
			t.Fatalf("items len = %d, want 1", len(result.Items))
		}
		item := result.Items[0]
		if item.BindStatus != stdomain.BindTypeManual {
			t.Errorf("bind_status = %q, want %q", item.BindStatus, stdomain.BindTypeManual)
		}
		if item.BoundNodeID != 11 || item.BoundNodeName != "SMT订单" {
			t.Errorf("bound node = %d/%q, want 11/SMT订单", item.BoundNodeID, item.BoundNodeName)
		}
		if item.BoundEnvID != 5 {
			t.Errorf("bound_env_id = %d, want 5", item.BoundEnvID)
		}
	})

	t.Run("指定环境 5：命中该环境绑定", func(t *testing.T) {
		result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{Conditions: conds, EnvID: 5})
		if err != nil {
			t.Fatalf("DryRunRules() error = %v", err)
		}
		if result.Items[0].BindStatus != stdomain.BindTypeManual {
			t.Errorf("bind_status = %q, want %q", result.Items[0].BindStatus, stdomain.BindTypeManual)
		}
	})

	t.Run("指定环境 6：该环境下视为未绑定", func(t *testing.T) {
		result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{Conditions: conds, EnvID: 6})
		if err != nil {
			t.Fatalf("DryRunRules() error = %v", err)
		}
		if result.Items[0].BindStatus != stdomain.BindStatusUnbound {
			t.Errorf("bind_status = %q, want %q", result.Items[0].BindStatus, stdomain.BindStatusUnbound)
		}
	})
}

// TestDryRunRulesReadOnly 只读：不写绑定、不改规则统计
func TestDryRunRulesReadOnly(t *testing.T) {
	// Arrange
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return dryRunTestInstances(), nil
		},
	}
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return nil, nil
		},
		createBatchFn: func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
			t.Error("DryRunRules 不应创建绑定")
			return 0, nil
		},
	}
	ruleRepo := &stubRuleRepo{}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo)

	// Act
	_, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	})
	if err != nil {
		t.Fatalf("DryRunRules() error = %v", err)
	}

	// Assert
	if len(ruleRepo.statsCalls) != 0 {
		t.Errorf("不应更新规则统计, got %v", ruleRepo.statsCalls)
	}
	if bindingRepo.createBatchHits != 0 {
		t.Errorf("CreateBatch 调用 %d 次, want 0", bindingRepo.createBatchHits)
	}
}

// TestDryRunRulesResultCap 数量上限保护：截断到 MaxDryRunItems，Total 保留真实命中总数
func TestDryRunRulesResultCap(t *testing.T) {
	// Arrange
	instances := make([]camdomain.Instance, 0, stdomain.MaxDryRunItems+1)
	for i := 0; i < int(stdomain.MaxDryRunItems)+1; i++ {
		instances = append(instances, camdomain.Instance{
			ID:         int64(i + 1),
			AssetID:    "i-bulk",
			AssetName:  "bulk-web",
			Attributes: map[string]any{},
		})
	}
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return instances, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return nil, nil
		},
	}, instanceRepo)

	// Act
	result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	})

	// Assert
	if err != nil {
		t.Fatalf("DryRunRules() error = %v", err)
	}
	if result.Total != int64(stdomain.MaxDryRunItems)+1 {
		t.Errorf("total = %d, want %d", result.Total, stdomain.MaxDryRunItems+1)
	}
	if int64(len(result.Items)) != stdomain.MaxDryRunItems {
		t.Errorf("items len = %d, want %d (截断)", len(result.Items), stdomain.MaxDryRunItems)
	}
	if !result.Capped {
		t.Error("capped 应为 true")
	}
}
