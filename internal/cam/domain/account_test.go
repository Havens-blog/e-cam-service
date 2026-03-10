package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudAccount_IsActive(t *testing.T) {
	tests := []struct {
		name   string
		status CloudAccountStatus
		want   bool
	}{
		{"活跃状态", CloudAccountStatusActive, true},
		{"禁用状态", CloudAccountStatusDisabled, false},
		{"错误状态", CloudAccountStatusError, false},
		{"测试中", CloudAccountStatusTesting, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &CloudAccount{Status: tt.status}
			assert.Equal(t, tt.want, a.IsActive())
		})
	}
}

func TestCloudAccount_IsReadOnly(t *testing.T) {
	a := &CloudAccount{Config: CloudAccountConfig{ReadOnly: true}}
	assert.True(t, a.IsReadOnly())

	a.Config.ReadOnly = false
	assert.False(t, a.IsReadOnly())
}

func TestCloudAccount_CanAutoSync(t *testing.T) {
	tests := []struct {
		name           string
		status         CloudAccountStatus
		enableAutoSync bool
		want           bool
	}{
		{"活跃+启用自动同步", CloudAccountStatusActive, true, true},
		{"活跃+未启用自动同步", CloudAccountStatusActive, false, false},
		{"禁用+启用自动同步", CloudAccountStatusDisabled, true, false},
		{"禁用+未启用自动同步", CloudAccountStatusDisabled, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &CloudAccount{
				Status: tt.status,
				Config: CloudAccountConfig{EnableAutoSync: tt.enableAutoSync},
			}
			assert.Equal(t, tt.want, a.CanAutoSync())
		})
	}
}

func TestCloudAccount_MaskSensitiveData(t *testing.T) {
	a := &CloudAccount{
		Name:            "test-account",
		AccessKeyID:     "AKID1234567890ABCDEF",
		AccessKeySecret: "secret-key-value",
	}

	masked := a.MaskSensitiveData()

	// 原始对象不变
	assert.Equal(t, "AKID1234567890ABCDEF", a.AccessKeyID)
	assert.Equal(t, "secret-key-value", a.AccessKeySecret)

	// 脱敏后
	assert.Equal(t, "***", masked.AccessKeySecret)
	assert.Equal(t, "AKID12***CDEF", masked.AccessKeyID)
	assert.Equal(t, "test-account", masked.Name) // 非敏感字段不变
}

func TestCloudAccount_MaskSensitiveData_ShortKey(t *testing.T) {
	a := &CloudAccount{
		AccessKeyID:     "short",
		AccessKeySecret: "secret",
	}
	masked := a.MaskSensitiveData()
	assert.Equal(t, "***", masked.AccessKeyID)
	assert.Equal(t, "***", masked.AccessKeySecret)
}

func TestCloudAccount_UpdateSyncStatus(t *testing.T) {
	a := &CloudAccount{}
	syncTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	a.UpdateSyncStatus(syncTime, 150)

	require.NotNil(t, a.LastSyncTime)
	assert.Equal(t, syncTime, *a.LastSyncTime)
	assert.Equal(t, int64(150), a.AssetCount)
	assert.False(t, a.UpdateTime.IsZero())
	assert.NotZero(t, a.UTime)
}

func TestCloudAccount_UpdateTestStatus(t *testing.T) {
	a := &CloudAccount{}
	testTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	a.UpdateTestStatus(testTime, CloudAccountStatusActive, "")

	require.NotNil(t, a.LastTestTime)
	assert.Equal(t, testTime, *a.LastTestTime)
	assert.Equal(t, CloudAccountStatusActive, a.Status)
	assert.Empty(t, a.ErrorMessage)

	// 测试失败场景
	a.UpdateTestStatus(testTime, CloudAccountStatusError, "connection refused")
	assert.Equal(t, CloudAccountStatusError, a.Status)
	assert.Equal(t, "connection refused", a.ErrorMessage)
}

func TestCloudAccount_Validate(t *testing.T) {
	tests := []struct {
		name    string
		account CloudAccount
		wantErr bool
	}{
		{
			name: "有效账号",
			account: CloudAccount{
				Name:            "test",
				Provider:        CloudProviderAliyun,
				AccessKeyID:     "AKID123",
				AccessKeySecret: "secret",
				TenantID:        "t-001",
			},
			wantErr: false,
		},
		{"缺少名称", CloudAccount{Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", TenantID: "t"}, true},
		{"缺少Provider", CloudAccount{Name: "n", AccessKeyID: "ak", AccessKeySecret: "sk", TenantID: "t"}, true},
		{"缺少AccessKeyID", CloudAccount{Name: "n", Provider: "aliyun", AccessKeySecret: "sk", TenantID: "t"}, true},
		{"缺少AccessKeySecret", CloudAccount{Name: "n", Provider: "aliyun", AccessKeyID: "ak", TenantID: "t"}, true},
		{"缺少TenantID", CloudAccount{Name: "n", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.account.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
