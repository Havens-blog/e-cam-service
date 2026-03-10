package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenant_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tenant  Tenant
		wantErr string
	}{
		{"有效租户", Tenant{ID: "t1", Name: "test"}, ""},
		{"缺少ID", Tenant{Name: "test"}, "tenant id cannot be empty"},
		{"ID过长", Tenant{ID: strings.Repeat("a", 51), Name: "test"}, "exceed 50"},
		{"缺少名称", Tenant{ID: "t1"}, "tenant name cannot be empty"},
		{"名称过长", Tenant{ID: "t1", Name: strings.Repeat("a", 101)}, "exceed 100"},
		{"DisplayName过长", Tenant{ID: "t1", Name: "ok", DisplayName: strings.Repeat("a", 201)}, "exceed 200"},
		{"Description过长", Tenant{ID: "t1", Name: "ok", Description: strings.Repeat("a", 501)}, "exceed 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tenant.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestTenant_IsActive(t *testing.T) {
	assert.True(t, (&Tenant{Status: TenantStatusActive}).IsActive())
	assert.False(t, (&Tenant{Status: TenantStatusInactive}).IsActive())
	assert.False(t, (&Tenant{Status: TenantStatusSuspended}).IsActive())
}

func TestTenant_CanCreateCloudAccount(t *testing.T) {
	// 无限制
	tenant := &Tenant{Settings: TenantSettings{MaxCloudAccounts: 0}}
	assert.True(t, tenant.CanCreateCloudAccount())

	// 未达上限
	tenant2 := &Tenant{
		Settings: TenantSettings{MaxCloudAccounts: 5},
		Metadata: TenantMetadata{CloudAccountCount: 3},
	}
	assert.True(t, tenant2.CanCreateCloudAccount())

	// 已达上限
	tenant3 := &Tenant{
		Settings: TenantSettings{MaxCloudAccounts: 5},
		Metadata: TenantMetadata{CloudAccountCount: 5},
	}
	assert.False(t, tenant3.CanCreateCloudAccount())
}

func TestTenant_CanCreateUser(t *testing.T) {
	assert.True(t, (&Tenant{Settings: TenantSettings{MaxUsers: 0}}).CanCreateUser())
	assert.True(t, (&Tenant{
		Settings: TenantSettings{MaxUsers: 10},
		Metadata: TenantMetadata{UserCount: 5},
	}).CanCreateUser())
	assert.False(t, (&Tenant{
		Settings: TenantSettings{MaxUsers: 10},
		Metadata: TenantMetadata{UserCount: 10},
	}).CanCreateUser())
}

func TestTenant_CanCreateUserGroup(t *testing.T) {
	assert.True(t, (&Tenant{Settings: TenantSettings{MaxUserGroups: 0}}).CanCreateUserGroup())
	assert.False(t, (&Tenant{
		Settings: TenantSettings{MaxUserGroups: 3},
		Metadata: TenantMetadata{UserGroupCount: 3},
	}).CanCreateUserGroup())
}

func TestTenant_IsProviderAllowed(t *testing.T) {
	// 无限制
	tenant := &Tenant{}
	assert.True(t, tenant.IsProviderAllowed(CloudProviderAliyun))

	// 有限制
	tenant2 := &Tenant{
		Settings: TenantSettings{AllowedProviders: []CloudProvider{CloudProviderAliyun, CloudProviderAWS}},
	}
	assert.True(t, tenant2.IsProviderAllowed(CloudProviderAliyun))
	assert.True(t, tenant2.IsProviderAllowed(CloudProviderAWS))
	assert.False(t, tenant2.IsProviderAllowed(CloudProviderTencent))
}

func TestTenant_IsFeatureEnabled(t *testing.T) {
	// nil features
	tenant := &Tenant{}
	assert.False(t, tenant.IsFeatureEnabled("cost_monitoring"))

	// feature enabled
	tenant2 := &Tenant{
		Settings: TenantSettings{Features: map[string]bool{"cost_monitoring": true, "iam_sync": false}},
	}
	assert.True(t, tenant2.IsFeatureEnabled("cost_monitoring"))
	assert.False(t, tenant2.IsFeatureEnabled("iam_sync"))
	assert.False(t, tenant2.IsFeatureEnabled("nonexistent"))
}
