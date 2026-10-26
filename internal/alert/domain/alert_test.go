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

// TestAlertEnumValues 枚举常量的字符串值即 Mongo(bson) 存量数据与前端展示契约，
// 误改常量值会导致历史告警事件无法匹配，此处固化当前值。
func TestAlertEnumValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"AlertTypeResourceChange", string(AlertTypeResourceChange), "resource_change"},
		{"AlertTypeSyncFailure", string(AlertTypeSyncFailure), "sync_failure"},
		{"AlertTypeExpiration", string(AlertTypeExpiration), "expiration"},
		{"AlertTypeSecurityGroup", string(AlertTypeSecurityGroup), "security_group"},
		{"SeverityInfo", string(SeverityInfo), "info"},
		{"SeverityWarning", string(SeverityWarning), "warning"},
		{"SeverityCritical", string(SeverityCritical), "critical"},
		{"EventStatusPending", string(EventStatusPending), "pending"},
		{"EventStatusSent", string(EventStatusSent), "sent"},
		{"EventStatusFailed", string(EventStatusFailed), "failed"},
		{"EventStatusSilenced", string(EventStatusSilenced), "silenced"},
		{"ChannelDingTalk", string(ChannelDingTalk), "dingtalk"},
		{"ChannelWeCom", string(ChannelWeCom), "wecom"},
		{"ChannelFeishu", string(ChannelFeishu), "feishu"},
		{"ChannelEmail", string(ChannelEmail), "email"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestAlertModelsJSONKeys 固化告警域对外模型的 json tag 键集合（API 契约）。
func TestAlertModelsJSONKeys(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "AlertRule",
			v:    AlertRule{},
			want: []string{"id", "name", "type", "condition", "channel_ids", "account_ids",
				"resource_types", "regions", "silence_duration", "escalate_after",
				"escalate_channels", "tenant_id", "enabled", "create_time", "update_time"},
		},
		{
			name: "AlertEvent",
			v:    AlertEvent{},
			want: []string{"id", "rule_id", "type", "severity", "title", "content", "source",
				"tenant_id", "status", "retry_count", "create_time", "sent_at"},
		},
		{
			name: "NotificationChannel",
			v:    NotificationChannel{},
			want: []string{"id", "name", "type", "config", "tenant_id", "enabled",
				"create_time", "update_time"},
		},
		{
			name: "ResourceChange",
			v:    ResourceChange{},
			want: []string{"change_type", "resource_type", "asset_id", "asset_name",
				"account_id", "provider", "region", "details"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONKeys(t, tc.v, tc.want)
		})
	}
}

// TestAlertRuleJSONRoundTrip 序列化→反序列化无损（当前 tag 行为）。
func TestAlertRuleJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	tests := []struct {
		name string
		rule AlertRule
	}{
		{
			name: "完整字段",
			rule: AlertRule{
				ID:               10,
				Name:             "资源变更告警",
				Type:             AlertTypeResourceChange,
				Condition:        map[string]any{"resource_types": []any{"ecs", "rds"}},
				ChannelIDs:       []int64{1, 2},
				AccountIDs:       []int64{3},
				ResourceTypes:    []string{"ecs"},
				Regions:          []string{"cn-hangzhou"},
				SilenceDuration:  30,
				EscalateAfter:    3,
				EscalateChannels: []int64{9},
				TenantID:         7,
				Enabled:          true,
				CreateTime:       now,
				UpdateTime:       now,
			},
		},
		{
			name: "零值（切片/映射为 null）",
			rule: AlertRule{ID: 11, Name: "空规则", TenantID: 7},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.rule)
			require.NoError(t, err)
			var got AlertRule
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tc.rule, got)
		})
	}
}

// TestAlertEventJSONRoundTrip 含 sent_at 指针的 nil / 非 nil 两态。
func TestAlertEventJSONRoundTrip(t *testing.T) {
	sentAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	createTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		event AlertEvent
	}{
		{
			name:  "sent_at 为 nil（待发送）",
			event: AlertEvent{ID: 1, RuleID: 2, Type: AlertTypeSyncFailure, Severity: SeverityCritical, Title: "同步失败", Content: map[string]any{"task_id": float64(123)}, Source: "sync_task:123", TenantID: 7, Status: EventStatusPending, CreateTime: createTime},
		},
		{
			name:  "sent_at 已设置（已发送）",
			event: AlertEvent{ID: 2, RuleID: 2, Type: AlertTypeExpiration, Severity: SeverityWarning, Status: EventStatusSent, RetryCount: 1, CreateTime: createTime, SentAt: &sentAt},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.event)
			require.NoError(t, err)
			var got AlertEvent
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tc.event, got)
		})
	}
}

// TestNotificationChannelAndResourceChangeRoundTrip 其余两个对外模型的无损往返。
func TestNotificationChannelAndResourceChangeRoundTrip(t *testing.T) {
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	channel := NotificationChannel{
		ID: 1, Name: "值班钉钉", Type: ChannelDingTalk,
		Config:   map[string]any{"webhook": "https://oapi.dingtalk.com/robot/send?access_token=x"},
		TenantID: 7, Enabled: true, CreateTime: now, UpdateTime: now,
	}
	b, err := json.Marshal(channel)
	require.NoError(t, err)
	var gotChannel NotificationChannel
	require.NoError(t, json.Unmarshal(b, &gotChannel))
	assert.Equal(t, channel, gotChannel)

	change := ResourceChange{
		ChangeType: "modified", ResourceType: "ecs", AssetID: "i-bp1x", AssetName: "web-01",
		AccountID: 3, Provider: "aliyun", Region: "cn-hangzhou",
		Details: map[string]any{"field": "instance_type"},
	}
	b, err = json.Marshal(change)
	require.NoError(t, err)
	var gotChange ResourceChange
	require.NoError(t, json.Unmarshal(b, &gotChange))
	assert.Equal(t, change, gotChange)
}
