package domain

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonKeys 序列化 v 并返回排序后的 JSON 顶层键集合。
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// assertJSONKeys 断言 v 序列化后的顶层键集合与 want 一致（want 无需预排序）。
func assertJSONKeys(t *testing.T, v any, want []string) {
	t.Helper()
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	assert.Equal(t, wantSorted, jsonKeys(t, v))
}

// TestCostModelsJSONKeys 固化成本域模型的 json tag 键集合。
// 注意多数模型同时携带 bson tag 且 ctime 键与 json create 命名有意不同
// （bson 用 ctime/utime，json 用 ctime/utime 直出），此处固化 json 契约。
func TestCostModelsJSONKeys(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "UnifiedBill",
			v:    UnifiedBill{},
			want: []string{"id", "provider", "account_id", "account_name", "billing_start",
				"billing_end", "service_type", "service_type_name", "resource_id",
				"resource_name", "region", "amount", "currency", "amount_cny",
				"charge_type", "tags", "tenant_id", "billing_date", "ctime", "utime"},
		},
		{
			name: "RawBillRecord",
			v:    RawBillRecord{},
			want: []string{"id", "account_id", "provider", "raw_data", "collect_id",
				"billing_date", "ctime"},
		},
		{
			name: "CostAllocation",
			v:    CostAllocation{},
			want: []string{"id", "dim_type", "dim_value", "dim_path", "node_id", "node_path",
				"period", "total_amount", "direct_amount", "shared_amount", "ratio_amount",
				"unallocated_flag", "default_flag", "rule_id", "tenant_id", "ctime"},
		},
		{
			name: "AllocationRule",
			v:    AllocationRule{},
			want: []string{"id", "name", "rule_type", "dimension_combos", "tag_key",
				"tag_value_map", "shared_config", "priority", "status", "tenant_id",
				"ctime", "utime"},
		},
		{
			name: "DimensionCombo",
			v:    DimensionCombo{},
			want: []string{"dimensions", "target_id", "target_name", "ratio"},
		},
		{
			name: "DimensionFilter",
			v:    DimensionFilter{},
			want: []string{"dim_type", "dim_value"},
		},
		{
			name: "DefaultAllocationPolicy",
			v:    DefaultAllocationPolicy{},
			want: []string{"id", "target_id", "target_name", "tenant_id", "ctime", "utime"},
		},
		{
			name: "DimensionHierarchy",
			v:    DimensionHierarchy{},
			want: []string{"id", "dim_type", "node_id", "node_name", "parent_id", "path",
				"tenant_id"},
		},
		{
			name: "SharedConfig",
			v:    SharedConfig{},
			want: []string{"resource_ids", "ratios"},
		},
		{
			name: "CostAnomaly",
			v:    CostAnomaly{},
			want: []string{"id", "dimension", "dimension_value", "anomaly_date",
				"actual_amount", "baseline_amount", "deviation_pct", "severity",
				"possible_cause", "tenant_id", "ctime"},
		},
		{
			name: "BudgetRule",
			v:    BudgetRule{},
			want: []string{"id", "name", "amount_limit", "period", "scope_type",
				"scope_value", "thresholds", "notified_at", "status", "tenant_id",
				"ctime", "utime"},
		},
		{
			name: "CollectLog",
			v:    CollectLog{},
			want: []string{"id", "account_id", "provider", "status", "start_time",
				"end_time", "bill_start", "bill_end", "record_count", "duration_ms",
				"error_msg", "tenant_id", "ctime"},
		},
		{
			name: "Recommendation",
			v:    Recommendation{},
			want: []string{"id", "type", "provider", "account_id", "resource_id",
				"resource_name", "region", "reason", "estimated_saving", "status",
				"dismissed_at", "dismiss_expiry", "tenant_id", "ctime", "utime"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONKeys(t, tc.v, tc.want)
		})
	}
}

// TestUnifiedBillJSONRoundTrip 统一账单全字段无损往返（含 time.Time 与 tags 映射）。
func TestUnifiedBillJSONRoundTrip(t *testing.T) {
	billingStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	billingEnd := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
	bill := UnifiedBill{
		ID: 1, Provider: "aliyun", AccountID: 3, AccountName: "主账号",
		BillingStart: billingStart, BillingEnd: billingEnd,
		ServiceType: ServiceTypeCompute, ServiceTypeName: "计算",
		ResourceID: "i-bp1x", ResourceName: "web-01", Region: "cn-hangzhou",
		Amount: 125.5, Currency: "CNY", AmountCNY: 125.5, ChargeType: "Subscription",
		Tags:     map[string]string{"env": "prod", "team": "infra"},
		TenantID: 7, BillingDate: "2026-01-01", CreateTime: 1700000000, UpdateTime: 1700000001,
	}
	b, err := json.Marshal(bill)
	require.NoError(t, err)
	var got UnifiedBill
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, bill, got)
}

// TestBudgetRuleNotifiedAtRoundTrip 预算规则 notified_at 映射（含 nil 两态）无损往返。
func TestBudgetRuleNotifiedAtRoundTrip(t *testing.T) {
	notified := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rule BudgetRule
	}{
		{
			name: "notified_at 已填充",
			rule: BudgetRule{
				ID: 1, Name: "月度预算", AmountLimit: 10000, Period: "monthly",
				ScopeType: "department", ScopeValue: "infra",
				Thresholds: []float64{50, 80, 100},
				NotifiedAt: map[string]time.Time{"80": notified},
				Status:     "active",
				TenantID:   7,
				CreateTime: 1700000000,
				UpdateTime: 1700000001,
			},
		},
		{
			name: "notified_at 为 nil（序列化为 null）",
			rule: BudgetRule{ID: 2, Name: "空预算", TenantID: 7},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.rule)
			require.NoError(t, err)
			var got BudgetRule
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tc.rule, got)
		})
	}
}

// TestRecommendationDismissPointers 优化建议 dismissed_at / dismiss_expiry
// 指针 nil 与已设置两态的无损往返（未忽略 vs 已忽略）。
func TestRecommendationDismissPointers(t *testing.T) {
	dismissedAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rec  Recommendation
	}{
		{
			name: "未忽略（双指针 nil）",
			rec:  Recommendation{ID: 1, Type: "rightsizing", Provider: "aliyun", ResourceID: "i-bp1x", EstimatedSaving: 30.5, Status: "open", TenantID: 7},
		},
		{
			name: "已忽略（双指针已设置）",
			rec:  Recommendation{ID: 2, Type: "rightsizing", ResourceID: "i-bp2x", Status: "dismissed", DismissedAt: &dismissedAt, DismissExpiry: &expiry, TenantID: 7},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.rec)
			require.NoError(t, err)
			var got Recommendation
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tc.rec, got)
		})
	}
}

// TestAllocationRuleSharedConfigRoundTrip 分摊规则含 SharedConfig 指针与
// 整数键 Ratios 映射的无损往返（nil / 已设置两态）。
func TestAllocationRuleSharedConfigRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		rule AllocationRule
	}{
		{
			name: "shared_config 为 nil",
			rule: AllocationRule{ID: 1, Name: "按部门直分", RuleType: "dimension",
				DimensionCombos: []DimensionCombo{{
					Dimensions: []DimensionFilter{{DimType: DimDepartment, DimValue: "infra"}},
					TargetID:   "12", TargetName: "基础架构部", Ratio: 100,
				}},
				Priority: 1, Status: "enabled", TenantID: 7,
			},
		},
		{
			name: "shared_config 已设置（含整数键 ratios）",
			rule: AllocationRule{ID: 2, Name: "共享带宽分摊", RuleType: "shared",
				TagKey: "team", TagValueMap: map[string]int64{"infra": 1},
				SharedConfig: &SharedConfig{
					ResourceIDs: []string{"cbwp-1", "cbwp-2"},
					Ratios:      map[int64]float64{12: 60, 13: 40},
				},
				Priority: 2, Status: "enabled", TenantID: 7,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.rule)
			require.NoError(t, err)
			var got AllocationRule
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tc.rule, got)
		})
	}
}
