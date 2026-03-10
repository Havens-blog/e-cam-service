package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstance_Validate(t *testing.T) {
	tests := []struct {
		name    string
		inst    Instance
		wantErr bool
	}{
		{
			name: "有效实例",
			inst: Instance{
				ModelUID: "ecs",
				AssetID:  "i-bp1234",
				TenantID: "tenant-001",
			},
			wantErr: false,
		},
		{
			name: "缺少ModelUID",
			inst: Instance{
				AssetID:  "i-bp1234",
				TenantID: "tenant-001",
			},
			wantErr: true,
		},
		{
			name: "缺少AssetID",
			inst: Instance{
				ModelUID: "ecs",
				TenantID: "tenant-001",
			},
			wantErr: true,
		},
		{
			name: "缺少TenantID",
			inst: Instance{
				ModelUID: "ecs",
				AssetID:  "i-bp1234",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.inst.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInstance_Attributes(t *testing.T) {
	inst := Instance{}

	// 空 attributes 时 GetAttribute 返回 false
	val, ok := inst.GetAttribute("key")
	assert.False(t, ok)
	assert.Nil(t, val)

	// SetAttribute 自动初始化 map
	inst.SetAttribute("provider", "aliyun")
	val, ok = inst.GetAttribute("provider")
	require.True(t, ok)
	assert.Equal(t, "aliyun", val)

	// GetStringAttribute
	assert.Equal(t, "aliyun", inst.GetStringAttribute("provider"))
	assert.Equal(t, "", inst.GetStringAttribute("nonexistent"))

	// GetIntAttribute
	inst.SetAttribute("cpu", 4)
	assert.Equal(t, int64(4), inst.GetIntAttribute("cpu"))

	// GetIntAttribute with float64 (JSON 反序列化常见)
	inst.SetAttribute("memory", float64(8192))
	assert.Equal(t, int64(8192), inst.GetIntAttribute("memory"))

	// GetIntAttribute with int64
	inst.SetAttribute("account_id", int64(100))
	assert.Equal(t, int64(100), inst.GetIntAttribute("account_id"))

	// GetIntAttribute 不存在的 key
	assert.Equal(t, int64(0), inst.GetIntAttribute("nonexistent"))

	// GetIntAttribute 类型不匹配
	inst.SetAttribute("name", "test")
	assert.Equal(t, int64(0), inst.GetIntAttribute("name"))
}

func TestInstance_GetStringAttribute_NonString(t *testing.T) {
	inst := Instance{
		Attributes: map[string]interface{}{
			"count": 42,
		},
	}
	// 非字符串类型返回空字符串
	assert.Equal(t, "", inst.GetStringAttribute("count"))
}
