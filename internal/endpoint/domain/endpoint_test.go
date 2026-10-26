package domain

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointJSONUsesFieldNames 当前行为：Endpoint 结构体无 json tag，
// 序列化键即 Go 字段名（导出字段的默认行为契约）。若未来补 tag 导致键变化，
// 该用例会先失败以提示同步消费方（端点同步/权限比对逻辑）。
func TestEndpointJSONUsesFieldNames(t *testing.T) {
	e := Endpoint{
		Id: 1, Path: "/api/v1/assets", Method: "GET",
		Resource: "asset", Desc: "资产列表",
		IsAuth: true, IsAudit: true, IsPermission: false,
	}
	b, err := json.Marshal(e)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"Desc", "Id", "IsAudit", "IsAuth", "IsPermission", "Method", "Path", "Resource"}
	assert.Equal(t, want, got)
	assert.Equal(t, "/api/v1/assets", m["Path"])
	assert.Equal(t, true, m["IsAuth"])
	assert.Equal(t, false, m["IsPermission"])
}

// TestEndpointJSONRoundTrip 全字段无损往返（布尔三标记为当前契约面）。
func TestEndpointJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   Endpoint
	}{
		{
			name: "全标记开启",
			in:   Endpoint{Id: 2, Path: "/api/v1/accounts", Method: "POST", Resource: "account", Desc: "创建云账号", IsAuth: true, IsAudit: true, IsPermission: true},
		},
		{
			name: "零值（全标记关闭）",
			in:   Endpoint{Id: 3, Path: "/healthz", Method: "GET"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.in)
			require.NoError(t, err)
			var got Endpoint
			require.NoError(t, json.Unmarshal(b, &got))
			assert.Equal(t, tc.in, got)
		})
	}
}
