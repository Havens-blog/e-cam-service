package allocation

import (
	"context"
	"testing"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock AllocationDAO ---

type mockAllocationDAO struct {
	createRuleFn              func(ctx context.Context, rule costdomain.AllocationRule) (int64, error)
	updateRuleFn              func(ctx context.Context, rule costdomain.AllocationRule) error
	getRuleByIDFn             func(ctx context.Context, id int64) (costdomain.AllocationRule, error)
	listRulesFn               func(ctx context.Context, filter repository.AllocationRuleFilter) ([]costdomain.AllocationRule, error)
	listActiveRulesFn         func(ctx context.Context, tenantID string) ([]costdomain.AllocationRule, error)
	deleteRuleFn              func(ctx context.Context, id int64) error
	saveDefaultPolicyFn       func(ctx context.Context, policy costdomain.DefaultAllocationPolicy) error
	getDefaultPolicyFn        func(ctx context.Context, tenantID string) (costdomain.DefaultAllocationPolicy, error)
	insertAllocationFn        func(ctx context.Context, alloc costdomain.CostAllocation) (int64, error)
	insertAllocationsFn       func(ctx context.Context, allocs []costdomain.CostAllocation) (int64, error)
	deleteAllocationsPeriodFn func(ctx context.Context, tenantID, period string) error
	listAllocationsFn         func(ctx context.Context, filter repository.AllocationFilter) ([]costdomain.CostAllocation, error)
	getAllocByDimFn           func(ctx context.Context, tenantID, dimType, dimValue, period string) ([]costdomain.CostAllocation, error)
	getAllocByNodeFn          func(ctx context.Context, tenantID string, nodeID int64, period string) ([]costdomain.CostAllocation, error)
}

func (m *mockAllocationDAO) CreateRule(ctx context.Context, rule costdomain.AllocationRule) (int64, error) {
	if m.createRuleFn != nil {
		return m.createRuleFn(ctx, rule)
	}
	return 1, nil
}
func (m *mockAllocationDAO) UpdateRule(ctx context.Context, rule costdomain.AllocationRule) error {
	if m.updateRuleFn != nil {
		return m.updateRuleFn(ctx, rule)
	}
	return nil
}
func (m *mockAllocationDAO) GetRuleByID(ctx context.Context, id int64) (costdomain.AllocationRule, error) {
	if m.getRuleByIDFn != nil {
		return m.getRuleByIDFn(ctx, id)
	}
	return costdomain.AllocationRule{}, nil
}
func (m *mockAllocationDAO) ListRules(ctx context.Context, filter repository.AllocationRuleFilter) ([]costdomain.AllocationRule, error) {
	if m.listRulesFn != nil {
		return m.listRulesFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockAllocationDAO) ListActiveRules(ctx context.Context, tenantID string) ([]costdomain.AllocationRule, error) {
	if m.listActiveRulesFn != nil {
		return m.listActiveRulesFn(ctx, tenantID)
	}
	return nil, nil
}
func (m *mockAllocationDAO) DeleteRule(ctx context.Context, id int64) error {
	if m.deleteRuleFn != nil {
		return m.deleteRuleFn(ctx, id)
	}
	return nil
}
func (m *mockAllocationDAO) SaveDefaultPolicy(ctx context.Context, policy costdomain.DefaultAllocationPolicy) error {
	if m.saveDefaultPolicyFn != nil {
		return m.saveDefaultPolicyFn(ctx, policy)
	}
	return nil
}
func (m *mockAllocationDAO) GetDefaultPolicy(ctx context.Context, tenantID string) (costdomain.DefaultAllocationPolicy, error) {
	if m.getDefaultPolicyFn != nil {
		return m.getDefaultPolicyFn(ctx, tenantID)
	}
	return costdomain.DefaultAllocationPolicy{}, nil
}
func (m *mockAllocationDAO) InsertAllocation(ctx context.Context, alloc costdomain.CostAllocation) (int64, error) {
	if m.insertAllocationFn != nil {
		return m.insertAllocationFn(ctx, alloc)
	}
	return 1, nil
}
func (m *mockAllocationDAO) InsertAllocations(ctx context.Context, allocs []costdomain.CostAllocation) (int64, error) {
	if m.insertAllocationsFn != nil {
		return m.insertAllocationsFn(ctx, allocs)
	}
	return int64(len(allocs)), nil
}
func (m *mockAllocationDAO) DeleteAllocationsByPeriod(ctx context.Context, tenantID, period string) error {
	if m.deleteAllocationsPeriodFn != nil {
		return m.deleteAllocationsPeriodFn(ctx, tenantID, period)
	}
	return nil
}
func (m *mockAllocationDAO) ListAllocations(ctx context.Context, filter repository.AllocationFilter) ([]costdomain.CostAllocation, error) {
	if m.listAllocationsFn != nil {
		return m.listAllocationsFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockAllocationDAO) GetAllocationByDimension(ctx context.Context, tenantID, dimType, dimValue, period string) ([]costdomain.CostAllocation, error) {
	if m.getAllocByDimFn != nil {
		return m.getAllocByDimFn(ctx, tenantID, dimType, dimValue, period)
	}
	return nil, nil
}
func (m *mockAllocationDAO) GetAllocationByNode(ctx context.Context, tenantID string, nodeID int64, period string) ([]costdomain.CostAllocation, error) {
	if m.getAllocByNodeFn != nil {
		return m.getAllocByNodeFn(ctx, tenantID, nodeID, period)
	}
	return nil, nil
}

// --- Mock BillDAO ---

type mockBillDAO struct {
	listUnifiedBillsFn func(ctx context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error)
	sumAmountFn        func(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error)
}

func (m *mockBillDAO) ListUnifiedBills(ctx context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
	if m.listUnifiedBillsFn != nil {
		return m.listUnifiedBillsFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockBillDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	if m.sumAmountFn != nil {
		return m.sumAmountFn(ctx, filter)
	}
	return 0, nil
}
func (m *mockBillDAO) InsertRawBill(_ context.Context, _ costdomain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) InsertRawBills(_ context.Context, _ []costdomain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) GetRawBillByID(_ context.Context, _ int64) (costdomain.RawBillRecord, error) {
	return costdomain.RawBillRecord{}, nil
}
func (m *mockBillDAO) ListRawBills(_ context.Context, _ int64, _, _ string) ([]costdomain.RawBillRecord, error) {
	return nil, nil
}
func (m *mockBillDAO) ListRawBillsByCollectID(_ context.Context, _ string) ([]costdomain.RawBillRecord, error) {
	return nil, nil
}
func (m *mockBillDAO) InsertUnifiedBill(_ context.Context, _ costdomain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) InsertUnifiedBills(_ context.Context, _ []costdomain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) GetUnifiedBillByID(_ context.Context, _ int64) (costdomain.UnifiedBill, error) {
	return costdomain.UnifiedBill{}, nil
}
func (m *mockBillDAO) CountUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) AggregateByField(_ context.Context, _ string, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.AggregateResult, error) {
	return nil, nil
}
func (m *mockBillDAO) AggregateDailyAmount(_ context.Context, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	return nil, nil
}
func (m *mockBillDAO) DeleteUnifiedBillsByPeriod(_ context.Context, _, _ string) error { return nil }
func (m *mockBillDAO) DeleteRawBillsByAccountAndRange(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) DeleteUnifiedBillsByAccountAndRange(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) AggregateByTag(_ context.Context, _ string, _, _ string) ([]repository.AggregateResult, error) {
	return nil, nil
}

// --- Test Setup ---

func setupTestService(t *testing.T) (*AllocationService, *mockAllocationDAO, *mockBillDAO) {
	t.Helper()
	allocDAO := &mockAllocationDAO{}
	billDAO := &mockBillDAO{}
	logger := elog.DefaultLogger
	svc := NewAllocationService(allocDAO, billDAO, logger)
	return svc, allocDAO, billDAO
}

// --- CreateAllocationRule Tests ---

func TestCreateAllocationRule_Success(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)
	var created costdomain.AllocationRule
	allocDAO.createRuleFn = func(_ context.Context, rule costdomain.AllocationRule) (int64, error) {
		created = rule
		return 1, nil
	}

	rule := costdomain.AllocationRule{
		Name:     "Test Rule",
		RuleType: "dimension_combo",
		DimensionCombos: []costdomain.DimensionCombo{
			{
				Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "us-east-1"}},
				TargetID:   "dept-1",
				TargetName: "Engineering",
				Ratio:      60,
			},
			{
				Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimServiceType, DimValue: "compute"}},
				TargetID:   "dept-2",
				TargetName: "Operations",
				Ratio:      40,
			},
		},
		TenantID: "tenant-1",
	}

	id, err := svc.CreateAllocationRule(context.Background(), rule)
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
	assert.Equal(t, "active", created.Status)
	assert.NotZero(t, created.CreateTime)
}

func TestCreateAllocationRule_EmptyName(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		RuleType: "dimension_combo",
		DimensionCombos: []costdomain.DimensionCombo{
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "us-east-1"}}, Ratio: 100},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name cannot be empty")
}

func TestCreateAllocationRule_ExceedMaxCombos(t *testing.T) {
	svc, _, _ := setupTestService(t)
	combos := make([]costdomain.DimensionCombo, 6)
	for i := range combos {
		combos[i] = costdomain.DimensionCombo{
			Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "r"}},
			Ratio:      100.0 / 6,
		}
	}
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:            "Too Many Combos",
		RuleType:        "dimension_combo",
		DimensionCombos: combos,
	})
	assert.ErrorIs(t, err, costdomain.ErrAllocationDimExceed)
}

func TestCreateAllocationRule_InvalidRatioSum(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:     "Bad Ratio",
		RuleType: "dimension_combo",
		DimensionCombos: []costdomain.DimensionCombo{
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "r"}}, Ratio: 50},
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "r2"}}, Ratio: 30},
		},
	})
	assert.ErrorIs(t, err, costdomain.ErrAllocationRatioInvalid)
}

func TestCreateAllocationRule_InvalidDimType(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:     "Invalid Dim",
		RuleType: "dimension_combo",
		DimensionCombos: []costdomain.DimensionCombo{
			{Dimensions: []costdomain.DimensionFilter{{DimType: "invalid_type", DimValue: "v"}}, Ratio: 100},
		},
	})
	assert.ErrorIs(t, err, costdomain.ErrAllocationDimInvalid)
}

func TestCreateAllocationRule_RatioSumWithTolerance(t *testing.T) {
	svc, _, _ := setupTestService(t)
	// 99.995 is within 0.01 tolerance of 100
	id, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:     "Tolerance Rule",
		RuleType: "dimension_combo",
		DimensionCombos: []costdomain.DimensionCombo{
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "r"}}, Ratio: 33.335},
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "r2"}}, Ratio: 33.33},
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "r3"}}, Ratio: 33.335},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
}

// --- UpdateAllocationRule Tests ---

func TestUpdateAllocationRule_Success(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)
	var updated costdomain.AllocationRule
	allocDAO.updateRuleFn = func(_ context.Context, rule costdomain.AllocationRule) error {
		updated = rule
		return nil
	}

	err := svc.UpdateAllocationRule(context.Background(), costdomain.AllocationRule{
		ID:       1,
		Name:     "Updated Rule",
		RuleType: "dimension_combo",
		DimensionCombos: []costdomain.DimensionCombo{
			{Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "us-west-2"}}, Ratio: 100},
		},
	})
	require.NoError(t, err)
	assert.NotZero(t, updated.UpdateTime)
}

// --- SetDefaultAllocationPolicy Tests ---

func TestSetDefaultAllocationPolicy_Success(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)
	var saved costdomain.DefaultAllocationPolicy
	allocDAO.saveDefaultPolicyFn = func(_ context.Context, p costdomain.DefaultAllocationPolicy) error {
		saved = p
		return nil
	}

	err := svc.SetDefaultAllocationPolicy(context.Background(), costdomain.DefaultAllocationPolicy{
		TargetID:   "default-dept",
		TargetName: "Default Department",
		TenantID:   "tenant-1",
	})
	require.NoError(t, err)
	assert.NotZero(t, saved.CreateTime)
	assert.Equal(t, "default-dept", saved.TargetID)
}

// --- AllocateCosts Tests ---

func TestAllocateCosts_WithMatchingRules(t *testing.T) {
	svc, allocDAO, billDAO := setupTestService(t)

	billDAO.listUnifiedBillsFn = func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
		return []costdomain.UnifiedBill{
			{ID: 1, Region: "us-east-1", AmountCNY: 1000, TenantID: "t1", ServiceType: "compute"},
		}, nil
	}

	allocDAO.listActiveRulesFn = func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
		return []costdomain.AllocationRule{
			{
				ID:       1,
				RuleType: "dimension_combo",
				DimensionCombos: []costdomain.DimensionCombo{
					{
						Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "us-east-1"}},
						TargetID:   "dept-1",
						Ratio:      60,
					},
					{
						Dimensions: []costdomain.DimensionFilter{{DimType: costdomain.DimRegion, DimValue: "us-east-1"}},
						TargetID:   "dept-2",
						Ratio:      40,
					},
				},
			},
		}, nil
	}

	var inserted []costdomain.CostAllocation
	allocDAO.insertAllocationsFn = func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
		inserted = allocs
		return int64(len(allocs)), nil
	}

	err := svc.AllocateCosts(context.Background(), "t1", "2024-01")
	require.NoError(t, err)
	require.Len(t, inserted, 2)
	assert.InDelta(t, 600, inserted[0].TotalAmount, 0.01)
	assert.InDelta(t, 400, inserted[1].TotalAmount, 0.01)
}

func TestAllocateCosts_DefaultPolicy(t *testing.T) {
	svc, allocDAO, billDAO := setupTestService(t)

	billDAO.listUnifiedBillsFn = func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
		return []costdomain.UnifiedBill{
			{ID: 1, Region: "ap-south-1", AmountCNY: 500, TenantID: "t1"},
		}, nil
	}

	allocDAO.listActiveRulesFn = func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
		return nil, nil // no rules
	}

	allocDAO.getDefaultPolicyFn = func(_ context.Context, _ string) (costdomain.DefaultAllocationPolicy, error) {
		return costdomain.DefaultAllocationPolicy{TargetID: "fallback-dept", TargetName: "Fallback"}, nil
	}

	var inserted []costdomain.CostAllocation
	allocDAO.insertAllocationsFn = func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
		inserted = allocs
		return int64(len(allocs)), nil
	}

	err := svc.AllocateCosts(context.Background(), "t1", "2024-01")
	require.NoError(t, err)
	require.Len(t, inserted, 1)
	assert.True(t, inserted[0].DefaultFlag)
	assert.Equal(t, "fallback-dept", inserted[0].DimValue)
	assert.InDelta(t, 500, inserted[0].TotalAmount, 0.01)
}

func TestAllocateCosts_Unallocated(t *testing.T) {
	svc, allocDAO, billDAO := setupTestService(t)

	billDAO.listUnifiedBillsFn = func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
		return []costdomain.UnifiedBill{
			{ID: 1, Region: "ap-south-1", AmountCNY: 300, TenantID: "t1"},
		}, nil
	}

	allocDAO.listActiveRulesFn = func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
		return nil, nil
	}

	// No default policy (returns empty)
	allocDAO.getDefaultPolicyFn = func(_ context.Context, _ string) (costdomain.DefaultAllocationPolicy, error) {
		return costdomain.DefaultAllocationPolicy{}, nil
	}

	var inserted []costdomain.CostAllocation
	allocDAO.insertAllocationsFn = func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
		inserted = allocs
		return int64(len(allocs)), nil
	}

	err := svc.AllocateCosts(context.Background(), "t1", "2024-01")
	require.NoError(t, err)
	require.Len(t, inserted, 1)
	assert.True(t, inserted[0].UnallocatedFlag)
	assert.Equal(t, "unallocated", inserted[0].DimValue)
}

// --- Tag-based Allocation Tests ---

func TestAllocateCosts_TagMapping(t *testing.T) {
	svc, allocDAO, billDAO := setupTestService(t)

	billDAO.listUnifiedBillsFn = func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
		return []costdomain.UnifiedBill{
			{ID: 1, AmountCNY: 800, TenantID: "t1", Tags: map[string]string{"env": "production"}},
		}, nil
	}

	allocDAO.listActiveRulesFn = func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
		return []costdomain.AllocationRule{
			{
				ID:       2,
				RuleType: "tag_mapping",
				TagKey:   "env",
				TagValueMap: map[string]int64{
					"production": 100,
					"staging":    200,
				},
			},
		}, nil
	}

	var inserted []costdomain.CostAllocation
	allocDAO.insertAllocationsFn = func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
		inserted = allocs
		return int64(len(allocs)), nil
	}

	err := svc.AllocateCosts(context.Background(), "t1", "2024-01")
	require.NoError(t, err)
	require.Len(t, inserted, 1)
	assert.Equal(t, int64(100), inserted[0].NodeID)
	assert.InDelta(t, 800, inserted[0].DirectAmount, 0.01)
	assert.Equal(t, costdomain.DimTag, inserted[0].DimType)
}

// --- Shared Resource Allocation Tests ---

func TestAllocateCosts_SharedRatio(t *testing.T) {
	svc, allocDAO, billDAO := setupTestService(t)

	billDAO.listUnifiedBillsFn = func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
		return []costdomain.UnifiedBill{
			{ID: 1, ResourceID: "shared-db-1", AmountCNY: 1000, TenantID: "t1"},
		}, nil
	}

	allocDAO.listActiveRulesFn = func(_ context.Context, _ string) ([]costdomain.AllocationRule, error) {
		return []costdomain.AllocationRule{
			{
				ID:       3,
				RuleType: "shared_ratio",
				SharedConfig: &costdomain.SharedConfig{
					ResourceIDs: []string{"shared-db-1"},
					Ratios:      map[int64]float64{10: 60, 20: 40},
				},
			},
		}, nil
	}

	var inserted []costdomain.CostAllocation
	allocDAO.insertAllocationsFn = func(_ context.Context, allocs []costdomain.CostAllocation) (int64, error) {
		inserted = allocs
		return int64(len(allocs)), nil
	}

	err := svc.AllocateCosts(context.Background(), "t1", "2024-01")
	require.NoError(t, err)
	require.Len(t, inserted, 2)

	// Sum should equal original amount
	totalShared := 0.0
	for _, a := range inserted {
		totalShared += a.SharedAmount
	}
	assert.InDelta(t, 1000, totalShared, 0.01)
}

// --- GetAllocationByDimension Tests ---

func TestGetAllocationByDimension(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)

	allocDAO.getAllocByDimFn = func(_ context.Context, _, _, _, _ string) ([]costdomain.CostAllocation, error) {
		return []costdomain.CostAllocation{
			{DimType: costdomain.DimRegion, DimValue: "us-east-1", TotalAmount: 500},
		}, nil
	}

	result, err := svc.GetAllocationByDimension(context.Background(), "t1", costdomain.DimRegion, "us-east-1", "2024-01")
	require.NoError(t, err)
	assert.Equal(t, costdomain.DimRegion, result.DimType)
	assert.Len(t, result.Allocations, 1)
	assert.InDelta(t, 500, result.Allocations[0].TotalAmount, 0.01)
}

// --- GetAllocationByNode Tests ---

func TestGetAllocationByNode(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)

	allocDAO.getAllocByNodeFn = func(_ context.Context, _ string, nodeID int64, _ string) ([]costdomain.CostAllocation, error) {
		return []costdomain.CostAllocation{
			{NodeID: nodeID, TotalAmount: 1200},
		}, nil
	}

	result, err := svc.GetAllocationByNode(context.Background(), "t1", 42, "2024-01")
	require.NoError(t, err)
	assert.Equal(t, int64(42), result.NodeID)
	assert.Len(t, result.Allocations, 1)
}

// --- ReAllocateHistory Tests ---

func TestReAllocateHistory(t *testing.T) {
	svc, allocDAO, billDAO := setupTestService(t)

	deleteCalled := false
	allocDAO.deleteAllocationsPeriodFn = func(_ context.Context, _, _ string) error {
		deleteCalled = true
		return nil
	}

	billDAO.listUnifiedBillsFn = func(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
		return nil, nil // empty bills
	}

	err := svc.ReAllocateHistory(context.Background(), "t1", "2023-06")
	require.NoError(t, err)
	assert.True(t, deleteCalled)
}

// --- ListRules Tests ---

func TestListRules(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)

	allocDAO.listRulesFn = func(_ context.Context, _ repository.AllocationRuleFilter) ([]costdomain.AllocationRule, error) {
		return []costdomain.AllocationRule{
			{ID: 1, Name: "Rule A"},
			{ID: 2, Name: "Rule B"},
		}, nil
	}

	rules, err := svc.ListRules(context.Background(), repository.AllocationRuleFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Len(t, rules, 2)
}

// --- DeleteRule Tests ---

func TestDeleteRule(t *testing.T) {
	svc, allocDAO, _ := setupTestService(t)

	deletedID := int64(0)
	allocDAO.deleteRuleFn = func(_ context.Context, id int64) error {
		deletedID = id
		return nil
	}

	err := svc.DeleteRule(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), deletedID)
}

// --- Tag Mapping Rule Validation ---

func TestCreateAllocationRule_TagMapping_EmptyTagKey(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:     "Tag Rule",
		RuleType: "tag_mapping",
		TagKey:   "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tag_key cannot be empty")
}

// --- Shared Ratio Rule Validation ---

func TestCreateAllocationRule_SharedRatio_NilConfig(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:         "Shared Rule",
		RuleType:     "shared_ratio",
		SharedConfig: nil,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shared_config cannot be nil")
}

func TestCreateAllocationRule_SharedRatio_InvalidRatioSum(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.CreateAllocationRule(context.Background(), costdomain.AllocationRule{
		Name:     "Shared Rule",
		RuleType: "shared_ratio",
		SharedConfig: &costdomain.SharedConfig{
			ResourceIDs: []string{"r1"},
			Ratios:      map[int64]float64{1: 50, 2: 30},
		},
	})
	assert.ErrorIs(t, err, costdomain.ErrAllocationRatioInvalid)
}

// --- periodEndDate Tests ---

func TestPeriodEndDate(t *testing.T) {
	svc, _, _ := setupTestService(t)
	assert.Equal(t, "2024-01-31", svc.periodEndDate("2024-01"))
	assert.Equal(t, "2024-02-29", svc.periodEndDate("2024-02")) // leap year
	assert.Equal(t, "2023-02-28", svc.periodEndDate("2023-02"))
	assert.Equal(t, "2024-12-31", svc.periodEndDate("2024-12"))
}
