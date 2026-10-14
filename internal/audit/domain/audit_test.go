package domain

import (
	"encoding/json"
	"sort"
	"testing"

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

// TestAuditOperationTypeValues 操作类型常量值即审计日志 Mongo 存量数据契约，
// 误改会导致历史审计记录无法按类型过滤，此处固化当前值。
func TestAuditOperationTypeValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"AuditOpAPIAccountCreate", string(AuditOpAPIAccountCreate), "api_account_create"},
		{"AuditOpAPIAccountUpdate", string(AuditOpAPIAccountUpdate), "api_account_update"},
		{"AuditOpAPIAccountDelete", string(AuditOpAPIAccountDelete), "api_account_delete"},
		{"AuditOpAPIAssetSync", string(AuditOpAPIAssetSync), "api_asset_sync"},
		{"AuditOpAPITaskCreate", string(AuditOpAPITaskCreate), "api_task_create"},
		{"AuditOpAPIAlertCreate", string(AuditOpAPIAlertCreate), "api_alert_create"},
		{"AuditOpAPIAlertUpdate", string(AuditOpAPIAlertUpdate), "api_alert_update"},
		{"AuditOpAPIAlertDelete", string(AuditOpAPIAlertDelete), "api_alert_delete"},
		{"AuditOpAPIGeneric", string(AuditOpAPIGeneric), "api_generic"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestAuditResultValues 审计结果枚举当前值。
func TestAuditResultValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"AuditResultSuccess", string(AuditResultSuccess), "success"},
		{"AuditResultFailed", string(AuditResultFailed), "failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestAuditModelsJSONKeys 固化审计域模型的 json tag 键集合。
// 注意 EndpointStats 的 bson 键为 "_id" 而 json 键为 "api_path"（当前双 tag 行为）。
func TestAuditModelsJSONKeys(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "AuditLog",
			v:    AuditLog{},
			want: []string{"id", "operation_type", "operator_id", "operator_name", "tenant_id",
				"http_method", "api_path", "request_body", "status_code", "result",
				"request_id", "duration_ms", "client_ip", "user_agent", "ctime"},
		},
		{
			name: "AuditReport",
			v:    AuditReport{},
			want: []string{"start_time", "end_time", "total_ops", "success_ops", "failed_ops",
				"ops_by_type", "ops_by_method", "top_endpoints", "error_by_endpoint",
				"top_operators", "generated_at"},
		},
		{
			name: "EndpointStats",
			v:    EndpointStats{},
			want: []string{"api_path", "count", "error_rate"},
		},
		{
			name: "OperatorStats",
			v:    OperatorStats{},
			want: []string{"operator_id", "operator_name", "op_count"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONKeys(t, tc.v, tc.want)
		})
	}
}

// TestAuditLogJSONRoundTrip 审计日志全字段无损往返（基础类型，无时间指针）。
func TestAuditLogJSONRoundTrip(t *testing.T) {
	log := AuditLog{
		ID:            1,
		OperationType: AuditOpAPIAccountCreate,
		OperatorID:    "u-1001",
		OperatorName:  "张三",
		TenantID:      7,
		HTTPMethod:    "POST",
		APIPath:       "/api/v1/accounts",
		RequestBody:   `{"name":"acct-1"}`,
		StatusCode:    200,
		Result:        AuditResultSuccess,
		RequestID:     "req-abc",
		DurationMs:    42,
		ClientIP:      "10.0.0.1",
		UserAgent:     "Mozilla/5.0",
		Ctime:         1700000000000,
	}
	b, err := json.Marshal(log)
	require.NoError(t, err)
	var got AuditLog
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, log, got)
}

// TestAuditReportJSONRoundTrip 报告含类型化 map 与嵌套统计切片的无损往返。
func TestAuditReportJSONRoundTrip(t *testing.T) {
	report := AuditReport{
		StartTime:  1700000000000,
		EndTime:    1700086400000,
		TotalOps:   10,
		SuccessOps: 8,
		FailedOps:  2,
		OpsByType: map[AuditOperationType]int64{
			AuditOpAPIAccountCreate: 3,
			AuditOpAPIGeneric:       7,
		},
		OpsByMethod:     map[string]int64{"GET": 4, "POST": 6},
		TopEndpoints:    []EndpointStats{{APIPath: "/api/v1/assets", Count: 5, ErrorRate: 0.2}},
		ErrorByEndpoint: []EndpointStats{{APIPath: "/api/v1/accounts", Count: 2, ErrorRate: 1}},
		TopOperators:    []OperatorStats{{OperatorID: "u-1001", OperatorName: "张三", OpCount: 5}},
		GeneratedAt:     1700086400001,
	}
	b, err := json.Marshal(report)
	require.NoError(t, err)
	var got AuditReport
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, report, got)
}
