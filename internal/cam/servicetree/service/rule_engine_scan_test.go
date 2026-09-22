package service

import (
	"context"
	"fmt"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
)

// 资产取数路径（listTenantInstances）单测：分页累积取全量 + 跨批去重。
// 背景：原实现固定 Limit 10000，租户资产超过该值时静默漏扫，绑定数随执行次数
// 逐批爬升而非一次收敛（DAO 按 ctime 倒序分页，非唯一键）。

// scanInstances 生成 [startID, startID+count) 的命中资产（名称含 web）
func scanInstances(startID int64, count int64) []camdomain.Instance {
	out := make([]camdomain.Instance, 0, count)
	for i := int64(0); i < count; i++ {
		id := startID + i
		out = append(out, camdomain.Instance{
			ID:        id,
			AssetID:   fmt.Sprintf("i-web-%d", id),
			AssetName: fmt.Sprintf("web-%d", id),
		})
	}
	return out
}

// TestListTenantInstancesPaginatesBeyondOneBatch 满批时继续翻页，命中数覆盖全量资产
func TestListTenantInstancesPaginatesBeyondOneBatch(t *testing.T) {
	// Arrange: 首批满批（触发继续翻页），第二批不足一批（末页终止）
	const tailCount = 3
	var gotOffsets []int64
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			gotOffsets = append(gotOffsets, filter.Offset)
			if filter.Offset == 0 {
				return scanInstances(1, instanceScanBatchSize), nil
			}
			return scanInstances(instanceScanBatchSize+1, tailCount), nil
		},
	}
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return nil, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	// Act
	result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	})

	// Assert
	if err != nil {
		t.Fatalf("DryRunRules() error = %v", err)
	}
	wantTotal := int64(instanceScanBatchSize + tailCount)
	if result.Total != wantTotal {
		t.Errorf("total = %d, want %d（分页未累积，仍只扫首批）", result.Total, wantTotal)
	}
	if len(gotOffsets) != 2 {
		t.Fatalf("List 调用次数 = %d, want 2（满批后应继续翻页，末页后终止）", len(gotOffsets))
	}
	if gotOffsets[0] != 0 || gotOffsets[1] != instanceScanBatchSize {
		t.Errorf("offsets = %v, want [0 %d]", gotOffsets, int64(instanceScanBatchSize))
	}
}

// TestListTenantInstancesDedupsAcrossBatches 跨批重复（ctime 排序非唯一键导致）按实例 ID 去重
func TestListTenantInstancesDedupsAcrossBatches(t *testing.T) {
	// Arrange: 第二批前两条与首批末两条重复，仅 1 条为新增
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			if filter.Offset == 0 {
				return scanInstances(1, instanceScanBatchSize), nil
			}
			dup := scanInstances(instanceScanBatchSize-1, 2) // 重复
			return append(dup, scanInstances(instanceScanBatchSize+1, 1)...), nil
		},
	}
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return nil, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	// Act
	result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	})

	// Assert
	if err != nil {
		t.Fatalf("DryRunRules() error = %v", err)
	}
	wantTotal := int64(instanceScanBatchSize + 1) // 重复项不计
	if result.Total != wantTotal {
		t.Errorf("total = %d, want %d（跨批重复未去重）", result.Total, wantTotal)
	}
}

// TestListTenantInstancesSinglePageStops 不足一批时单次查询即终止（小租户不做无谓翻页）
func TestListTenantInstancesSinglePageStops(t *testing.T) {
	// Arrange
	calls := 0
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			calls++
			return scanInstances(1, 2), nil
		},
	}
	bindingRepo := &stubBindingRepo{
		listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
			return nil, nil
		},
	}
	s := newTestRuleEngine(&stubRuleRepo{}, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	// Act
	result, err := s.DryRunRules(context.Background(), 1, stdomain.DryRunRequest{
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	})

	// Assert
	if err != nil {
		t.Fatalf("DryRunRules() error = %v", err)
	}
	if calls != 1 {
		t.Errorf("List 调用次数 = %d, want 1（末页应立即终止）", calls)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
}
