package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChangeOrderAuditEntryOmitEmpty 固化 7.2 变更单审计契约：
// - 订单级事件 item_id 为空，不出现在序列化结果中；项级事件必填出现；
// - orphan_cleanup / verify 载荷字段与 DedupKey 仅在非空时序列化（omitempty）；
// - success 为非 nil 指针时即使 false 也必须序列化（omitempty 不作用于指针）。
func TestChangeOrderAuditEntryOmitEmpty(t *testing.T) {
	success := false
	tests := []struct {
		name    string
		entry   ChangeOrderAuditEntry
		present []string // 必须出现的键
		absent  []string // 必须不出现的键
	}{
		{
			name:    "订单级 create：载荷全空全省略",
			entry:   ChangeOrderAuditEntry{ID: 1, OrderID: "CO-1", Actor: "u-1001", Action: "create", Detail: "创建变更单", At: 100},
			present: []string{"id", "order_id", "actor", "action", "detail", "at"},
			absent:  []string{"item_id", "cloud", "cloud_cert_id", "orphan_action", "success", "unmet_domains", "dedup_key"},
		},
		{
			name:    "项级 item_result：item_id 出现",
			entry:   ChangeOrderAuditEntry{ID: 2, OrderID: "CO-1", Actor: "u-1001", Action: "item_result", Detail: "执行成功", ItemID: "item-1", At: 200},
			present: []string{"item_id"},
			absent:  []string{"cloud", "cloud_cert_id", "orphan_action", "success", "unmet_domains", "dedup_key"},
		},
		{
			name:    "orphan_cleanup：载荷与 dedup_key 出现",
			entry:   ChangeOrderAuditEntry{ID: 3, OrderID: "CO-2", Actor: "executor", Action: "orphan_cleanup", Detail: "清理孤儿记录", Cloud: "aliyun", CloudCertID: "cert-1", OrphanAction: "cleanup", Success: &success, DedupKey: "cert-1|cleanup|false", At: 300},
			present: []string{"cloud", "cloud_cert_id", "orphan_action", "success", "dedup_key"},
			absent:  []string{"item_id", "unmet_domains"},
		},
		{
			name:    "verify：unmet_domains 与 dedup_key 出现",
			entry:   ChangeOrderAuditEntry{ID: 4, OrderID: "CO-3", Actor: "scheduler", Action: "verify", Detail: "验证未达标", UnmetDomains: []string{"a.example.com"}, DedupKey: "1700000000000", At: 400},
			present: []string{"unmet_domains", "dedup_key"},
			absent:  []string{"item_id", "cloud", "cloud_cert_id", "orphan_action", "success"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.entry)
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, json.Unmarshal(b, &m))
			for _, k := range tc.present {
				assert.Contains(t, m, k)
			}
			for _, k := range tc.absent {
				assert.NotContains(t, m, k)
			}
		})
	}
}

// TestChangeOrderAuditEntrySuccessPointerFalse success=false（非 nil 指针，零值）
// 必须序列化为 false 而非被 omitempty 吞掉——否则 5.9 报告存档会把失败误读为成功。
func TestChangeOrderAuditEntrySuccessPointerFalse(t *testing.T) {
	success := false
	b, err := json.Marshal(ChangeOrderAuditEntry{
		OrderID: "CO-4", Actor: "executor", Action: "orphan_cleanup",
		OrphanAction: "cleanup", Success: &success,
	})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Contains(t, m, "success")
	assert.Equal(t, false, m["success"])
}

// TestChangeOrderAuditEntryJSONRoundTrip 含载荷与去重键的全字段无损往返。
func TestChangeOrderAuditEntryJSONRoundTrip(t *testing.T) {
	success := true
	entry := ChangeOrderAuditEntry{
		ID: 5, OrderID: "CO-5", Actor: "executor", Action: "orphan_cleanup",
		Detail: "清理 3 条孤儿记录", ItemID: "item-9", At: 500,
		Cloud: "aws", CloudCertID: "arn:aws:acm:us-east-1:123", OrphanAction: "skip_keep",
		Success: &success, UnmetDomains: []string{"b.example.com", "c.example.com"},
		DedupKey: "arn:aws:acm:us-east-1:123|skip_keep|true",
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	var got ChangeOrderAuditEntry
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, entry, got)
}
