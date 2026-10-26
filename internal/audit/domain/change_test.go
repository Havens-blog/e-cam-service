package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChangeModelsJSONKeys 固化变更记录域模型的 json tag 键集合。
func TestChangeModelsJSONKeys(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "ChangeRecord",
			v:    ChangeRecord{},
			want: []string{"id", "asset_id", "asset_name", "model_uid", "tenant_id",
				"account_id", "provider", "region", "field_name", "old_value", "new_value",
				"change_source", "change_task_id", "ctime"},
		},
		{
			name: "ChangeFilter",
			v:    ChangeFilter{},
			want: []string{"asset_id", "tenant_id", "model_uid", "provider", "field_name",
				"start_time", "end_time", "offset", "limit"},
		},
		{
			name: "ChangeSummary",
			v:    ChangeSummary{},
			want: []string{"by_resource_type", "by_field", "by_provider", "total"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONKeys(t, tc.v, tc.want)
		})
	}
}

// TestChangeSummaryNilMapsJSONNull 当前行为：零值 ChangeSummary 的 nil map
// 序列化为 null（非空对象），消费方按 null 容错；改动前会在此处失败提示。
func TestChangeSummaryNilMapsJSONNull(t *testing.T) {
	b, err := json.Marshal(ChangeSummary{})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Nil(t, m["by_resource_type"])
	assert.Nil(t, m["by_field"])
	assert.Nil(t, m["by_provider"])
	assert.Equal(t, float64(0), m["total"])
}

// TestChangeRecordJSONRoundTrip 变更记录全字段无损往返。
func TestChangeRecordJSONRoundTrip(t *testing.T) {
	record := ChangeRecord{
		ID: 1, AssetID: "i-bp1x", AssetName: "web-01", ModelUID: "ecs",
		TenantID: 7, AccountID: 3, Provider: "aliyun", Region: "cn-hangzhou",
		FieldName: "instance_type", OldValue: `"ecs.c5.large"`, NewValue: `"ecs.c6.large"`,
		ChangeSource: "sync", ChangeTaskID: "task-1", Ctime: 1700000000000,
	}
	b, err := json.Marshal(record)
	require.NoError(t, err)
	var got ChangeRecord
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, record, got)
}

// TestChangeSummaryJSONRoundTrip 汇总映射无损往返。
func TestChangeSummaryJSONRoundTrip(t *testing.T) {
	summary := ChangeSummary{
		ByResourceType: map[string]int64{"ecs": 3, "rds": 1},
		ByField:        map[string]int64{"instance_type": 2},
		ByProvider:     map[string]int64{"aliyun": 4},
		Total:          4,
	}
	b, err := json.Marshal(summary)
	require.NoError(t, err)
	var got ChangeSummary
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, summary, got)
}
