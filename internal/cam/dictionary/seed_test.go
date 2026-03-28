package dictionary

import (
	"context"
	"regexp"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/stretchr/testify/assert"
)

// TestSeedTypes_Count 验证种子数据包含 19 个字典类型
func TestSeedTypes_Count(t *testing.T) {
	assert.Equal(t, 19, len(seedTypes), "seedTypes should contain exactly 19 dictionary types")
}

// TestSeedTypes_StructuralIntegrity 验证所有字段非空、sort_order 有效、value 格式正确
func TestSeedTypes_StructuralIntegrity(t *testing.T) {
	valuePattern := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	for _, st := range seedTypes {
		t.Run(st.Code, func(t *testing.T) {
			assert.NotEmpty(t, st.Code, "code should not be empty")
			assert.NotEmpty(t, st.Name, "name should not be empty")
			assert.NotEmpty(t, st.Description, "description should not be empty")
			assert.NotEmpty(t, st.Items, "items should not be empty")

			for i, item := range st.Items {
				assert.NotEmpty(t, item.Value, "item value should not be empty")
				assert.NotEmpty(t, item.Label, "item label should not be empty")
				assert.Equal(t, i+1, item.SortOrder, "sort_order should be %d for item %s", i+1, item.Value)
				assert.True(t, valuePattern.MatchString(item.Value),
					"value %q should match ^[a-z][a-z0-9_]*$", item.Value)
			}
		})
	}
}

// TestSeedTypes_CloudProviderConsistency 验证 cloud_provider 的 value 与后端 CloudProvider 常量一致
func TestSeedTypes_CloudProviderConsistency(t *testing.T) {
	var cpType *SeedType
	for i := range seedTypes {
		if seedTypes[i].Code == "cloud_provider" {
			cpType = &seedTypes[i]
			break
		}
	}
	assert.NotNil(t, cpType, "cloud_provider seed type should exist")

	values := make(map[string]bool)
	for _, item := range cpType.Items {
		values[item.Value] = true
	}

	// 验证与 domain.CloudProvider 常量一致
	assert.True(t, values[string(domain.CloudProviderAliyun)], "should contain aliyun")
	assert.True(t, values[string(domain.CloudProviderAWS)], "should contain aws")
	assert.True(t, values[string(domain.CloudProviderAzure)], "should contain azure")
	assert.True(t, values[string(domain.CloudProviderTencent)], "should contain tencent")
	assert.True(t, values[string(domain.CloudProviderHuawei)], "should contain huawei")
	// volcano 是额外的，不在 domain 常量中
	assert.True(t, values["volcano"], "should contain volcano")
}

// TestSeedTypes_AccountStatusConsistency 验证 account_status 的 value 与后端 CloudAccountStatus 常量一致
func TestSeedTypes_AccountStatusConsistency(t *testing.T) {
	var asType *SeedType
	for i := range seedTypes {
		if seedTypes[i].Code == "account_status" {
			asType = &seedTypes[i]
			break
		}
	}
	assert.NotNil(t, asType, "account_status seed type should exist")

	values := make(map[string]bool)
	for _, item := range asType.Items {
		values[item.Value] = true
	}

	assert.True(t, values[string(domain.CloudAccountStatusActive)], "should contain active")
	assert.True(t, values[string(domain.CloudAccountStatusDisabled)], "should contain disabled")
	assert.True(t, values[string(domain.CloudAccountStatusError)], "should contain error")
	assert.True(t, values[string(domain.CloudAccountStatusTesting)], "should contain testing")
	// inactive 是额外的，不在 domain 常量中
	assert.True(t, values["inactive"], "should contain inactive")
}

// TestSeedTypes_EnvironmentConsistency 验证 environment 的 value 与后端 Environment 常量一致
func TestSeedTypes_EnvironmentConsistency(t *testing.T) {
	var envType *SeedType
	for i := range seedTypes {
		if seedTypes[i].Code == "environment" {
			envType = &seedTypes[i]
			break
		}
	}
	assert.NotNil(t, envType, "environment seed type should exist")

	values := make(map[string]bool)
	for _, item := range envType.Items {
		values[item.Value] = true
	}

	assert.True(t, values[string(domain.EnvironmentProduction)], "should contain production")
	assert.True(t, values[string(domain.EnvironmentStaging)], "should contain staging")
	assert.True(t, values[string(domain.EnvironmentDevelopment)], "should contain development")
	// testing, dr, sandbox 是额外的
	assert.True(t, values["testing"], "should contain testing")
	assert.True(t, values["dr"], "should contain dr")
	assert.True(t, values["sandbox"], "should contain sandbox")
}

// TestSeedDictData_CreatesAllTypes 使用 inMemoryDAO 验证所有 19 个类型被创建
func TestSeedDictData_CreatesAllTypes(t *testing.T) {
	dao := newInMemoryDAO()
	svc := NewDictService(dao)
	ctx := context.Background()

	created, skipped, err := SeedDictData(ctx, svc, "default")
	assert.NoError(t, err)
	assert.Equal(t, 19, created, "should create all 19 types")
	assert.Equal(t, 0, skipped, "should skip no types on empty DB")

	// 验证每个类型都可以通过 GetByCode 查询到
	for _, st := range seedTypes {
		items, err := svc.GetByCode(ctx, "default", st.Code)
		assert.NoError(t, err)
		assert.Equal(t, len(st.Items), len(items), "type %s should have %d items", st.Code, len(st.Items))
	}
}

// TestSeedDictData_Idempotent 执行两次验证幂等性
func TestSeedDictData_Idempotent(t *testing.T) {
	dao := newInMemoryDAO()
	svc := NewDictService(dao)
	ctx := context.Background()

	// 第一次执行
	created1, skipped1, err := SeedDictData(ctx, svc, "default")
	assert.NoError(t, err)
	assert.Equal(t, 19, created1)
	assert.Equal(t, 0, skipped1)

	// 第二次执行
	created2, skipped2, err := SeedDictData(ctx, svc, "default")
	assert.NoError(t, err)
	assert.Equal(t, 0, created2, "second run should create nothing")
	assert.Equal(t, 19, skipped2, "second run should skip all 19 types")

	// 验证数据不变
	for _, st := range seedTypes {
		items, err := svc.GetByCode(ctx, "default", st.Code)
		assert.NoError(t, err)
		assert.Equal(t, len(st.Items), len(items), "type %s item count should be unchanged", st.Code)
	}
}

// TestSeedDictData_SkipsExisting 预创建部分类型，验证跳过
func TestSeedDictData_SkipsExisting(t *testing.T) {
	dao := newInMemoryDAO()
	svc := NewDictService(dao)
	ctx := context.Background()

	// 预创建 3 个类型
	preCreated := []string{"cloud_provider", "asset_status", "environment"}
	for _, code := range preCreated {
		for _, st := range seedTypes {
			if st.Code == code {
				_, err := svc.CreateType(ctx, "default", CreateTypeReq{
					Code:        st.Code,
					Name:        st.Name,
					Description: st.Description,
				})
				assert.NoError(t, err)
				break
			}
		}
	}

	created, skipped, err := SeedDictData(ctx, svc, "default")
	assert.NoError(t, err)
	assert.Equal(t, 16, created, "should create 16 new types")
	assert.Equal(t, 3, skipped, "should skip 3 pre-existing types")
}

// TestSeedDictData_CountInvariant 验证 created + skipped == 19
func TestSeedDictData_CountInvariant(t *testing.T) {
	dao := newInMemoryDAO()
	svc := NewDictService(dao)
	ctx := context.Background()

	// 预创建 5 个类型
	for i, st := range seedTypes {
		if i >= 5 {
			break
		}
		_, err := svc.CreateType(ctx, "default", CreateTypeReq{
			Code:        st.Code,
			Name:        st.Name,
			Description: st.Description,
		})
		assert.NoError(t, err)
	}

	created, skipped, err := SeedDictData(ctx, svc, "default")
	assert.NoError(t, err)
	assert.Equal(t, 19, created+skipped, "created + skipped should equal total seed types (19)")
}
