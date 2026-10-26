package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCloudProviderValues 云厂商标识常量值即账单数据按 provider 归类的存储契约，
// 误改会导致历史账单无法归属，此处固化当前值。
func TestCloudProviderValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"CloudProviderAliyun", string(CloudProviderAliyun), "aliyun"},
		{"CloudProviderAWS", string(CloudProviderAWS), "aws"},
		{"CloudProviderVolcano", string(CloudProviderVolcano), "volcano"},
		{"CloudProviderHuawei", string(CloudProviderHuawei), "huawei"},
		{"CloudProviderTencent", string(CloudProviderTencent), "tencent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestServiceTypeValues 统一服务分类常量当前值（成本看板分组维度）。
func TestServiceTypeValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"ServiceTypeCompute", ServiceTypeCompute, "compute"},
		{"ServiceTypeStorage", ServiceTypeStorage, "storage"},
		{"ServiceTypeNetwork", ServiceTypeNetwork, "network"},
		{"ServiceTypeDatabase", ServiceTypeDatabase, "database"},
		{"ServiceTypeMiddleware", ServiceTypeMiddleware, "middleware"},
		{"ServiceTypeOther", ServiceTypeOther, "other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// TestAllocationDimensionTypeValues 分摊维度类型常量当前值（规则引擎分支依据）。
func TestAllocationDimensionTypeValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"DimDepartment", DimDepartment, "department"},
		{"DimResourceGroup", DimResourceGroup, "resource_group"},
		{"DimProject", DimProject, "project"},
		{"DimTag", DimTag, "tag"},
		{"DimCloudAccount", DimCloudAccount, "cloud_account"},
		{"DimRegion", DimRegion, "region"},
		{"DimServiceType", DimServiceType, "service_type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
