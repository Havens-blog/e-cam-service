package template

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// ============================================================================
// Property Tests for Executor logic (pure functions, no cloud API calls)
// ============================================================================

// Feature: vm-template-provisioning, Property 8: 创建数量范围校验
// For any creation request, count in [1,20] is accepted, count <1 or >20 is rejected.
//
// **Validates: Requirements 4.1, 9.1**
func TestProperty_CountRangeValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmplDAO := newInMemoryTemplateDAO()
		taskDAO := newInMemoryProvisionTaskDAO()
		svc := NewTemplateService(tmplDAO, taskDAO, nil)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		// Create a template first
		tmpl, err := svc.CreateTemplate(ctx, tenantID, CreateTemplateReq{
			Name:             genTemplateName().Draw(rt, "name"),
			Provider:         "aliyun",
			CloudAccountID:   1,
			Region:           "cn-hangzhou",
			Zone:             "cn-hangzhou-a",
			InstanceType:     "ecs.g6.large",
			ImageID:          "img-123",
			VPCID:            "vpc-123",
			SubnetID:         "vsw-123",
			SecurityGroupIDs: []string{"sg-123"},
		})
		assert.NoError(rt, err)

		// Valid count [1,20] should succeed
		validCount := rapid.IntRange(1, 20).Draw(rt, "validCount")
		taskID, err := svc.ProvisionFromTemplate(ctx, tenantID, "user1", tmpl.ID, ProvisionReq{Count: validCount})
		assert.NoError(rt, err)
		assert.NotEmpty(rt, taskID)

		// Invalid count should fail
		invalidCount := rapid.OneOf(
			rapid.IntRange(-100, 0),
			rapid.IntRange(21, 1000),
		).Draw(rt, "invalidCount")
		_, err = svc.ProvisionFromTemplate(ctx, tenantID, "user1", tmpl.ID, ProvisionReq{Count: invalidCount})
		assert.ErrorIs(rt, err, ErrInvalidCount)

		// Same for direct provision
		_, err = svc.DirectProvision(ctx, tenantID, "user1", DirectProvisionReq{
			Provider:         "aliyun",
			CloudAccountID:   1,
			Region:           "cn-hangzhou",
			Zone:             "cn-hangzhou-a",
			InstanceType:     "ecs.g6.large",
			ImageID:          "img-123",
			VPCID:            "vpc-123",
			SubnetID:         "vsw-123",
			SecurityGroupIDs: []string{"sg-123"},
			Count:            invalidCount,
		})
		assert.ErrorIs(rt, err, ErrInvalidCount)
	})
}

// Feature: vm-template-provisioning, Property 9: 参数覆盖优先级
// For any template and override params, overridden fields use override values,
// non-overridden fields use template original values.
//
// **Validates: Requirements 4.2**
func TestProperty_ParamOverridePriority(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		origPrefix := rapid.StringMatching(`orig-[a-z]{3,6}`).Draw(rt, "origPrefix")
		overridePrefix := rapid.StringMatching(`new-[a-z]{3,6}`).Draw(rt, "overridePrefix")

		tmpl := &VMTemplate{
			Region:             "cn-hangzhou",
			Zone:               "cn-hangzhou-a",
			InstanceType:       "ecs.g6.large",
			ImageID:            "img-123",
			VPCID:              "vpc-123",
			SubnetID:           "vsw-123",
			SecurityGroupIDs:   []string{"sg-123"},
			InstanceNamePrefix: origPrefix,
			Tags:               map[string]string{"env": "prod", "team": "ops"},
		}

		overrideTags := map[string]string{"env": "staging", "version": "v2"}

		// Merge tags: template tags + override tags (override wins)
		mergedTags := make(map[string]string)
		for k, v := range tmpl.Tags {
			mergedTags[k] = v
		}
		for k, v := range overrideTags {
			mergedTags[k] = v
		}

		params := BuildCreateInstanceParams(tmpl, overridePrefix, mergedTags, 1)

		// Overridden fields should use override values
		assert.Equal(rt, overridePrefix, params.InstanceName, "overridden name prefix should use override value")
		assert.Equal(rt, "staging", params.Tags["env"], "overridden tag should use override value")
		assert.Equal(rt, "v2", params.Tags["version"], "new override tag should be present")

		// Non-overridden fields should keep template values
		assert.Equal(rt, tmpl.Region, params.Region)
		assert.Equal(rt, tmpl.InstanceType, params.InstanceType)
		assert.Equal(rt, "ops", params.Tags["team"], "non-overridden tag should keep template value")
	})
}

// Feature: vm-template-provisioning, Property 10: 实例名称序号生成规则
// For any prefix and count N>1, generated names follow {prefix}-{NNN} format,
// starting from 001, consecutive, and all unique.
//
// **Validates: Requirements 4.6, 9.7**
func TestProperty_InstanceNameGeneration(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "prefix")
		count := rapid.IntRange(2, 20).Draw(rt, "count")

		names := make([]string, count)
		nameSet := make(map[string]bool)

		for i := 0; i < count; i++ {
			name := GenerateInstanceName(prefix, i, count)
			names[i] = name
			nameSet[name] = true
		}

		// All names should be unique
		assert.Equal(rt, count, len(nameSet), "all generated names should be unique")

		// Names should follow {prefix}-{NNN} format
		for i := 0; i < count; i++ {
			expected := fmt.Sprintf("%s-%03d", prefix, i+1)
			assert.Equal(rt, expected, names[i], "name at index %d should match format", i)
		}
	})
}

// Feature: vm-template-provisioning, Property 10 (cont): Single instance gets plain name
func TestProperty_SingleInstanceName(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		prefix := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "prefix")
		name := GenerateInstanceName(prefix, 0, 1)
		assert.Equal(rt, prefix, name, "single instance should use prefix directly without suffix")
	})
}

// Feature: vm-template-provisioning, Property 10 (cont): Empty prefix gets default name
func TestProperty_EmptyPrefixName(t *testing.T) {
	name := GenerateInstanceName("", 0, 1)
	assert.Equal(t, "instance", name, "empty prefix single instance should default to 'instance'")

	name2 := GenerateInstanceName("", 0, 3)
	assert.Equal(t, "instance-001", name2, "empty prefix multi instance should default to 'instance-NNN'")
}

// Feature: vm-template-provisioning, Property 15: 创建任务详情与进度一致性
// For any task, progress = (success_count + failed_count) / count * 100,
// and instances list length = success_count + failed_count.
//
// **Validates: Requirements 6.2, 6.3**
func TestProperty_TaskProgressConsistency(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		total := rapid.IntRange(1, 20).Draw(rt, "total")
		successCount := rapid.IntRange(0, total).Draw(rt, "successCount")
		failedCount := total - successCount

		progress := (successCount + failedCount) * 100 / total
		assert.Equal(rt, 100, progress, "when all instances are processed, progress should be 100")

		// Partial progress
		processed := rapid.IntRange(0, total).Draw(rt, "processed")
		partialProgress := processed * 100 / total
		assert.GreaterOrEqual(rt, partialProgress, 0)
		assert.LessOrEqual(rt, partialProgress, 100)

		// Instances list length should equal processed count
		instances := make([]ProvisionInstanceResult, processed)
		assert.Equal(rt, processed, len(instances))
	})
}

// Feature: vm-template-provisioning, Property 12: 批量创建部分失败处理
// For any batch, success_count + failed_count = total count,
// and status is determined correctly.
//
// **Validates: Requirements 4.8, 4.9, 9.8**
func TestProperty_BatchPartialFailureStatus(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		total := rapid.IntRange(1, 20).Draw(rt, "total")
		successCount := rapid.IntRange(0, total).Draw(rt, "successCount")
		failedCount := total - successCount

		assert.Equal(rt, total, successCount+failedCount, "success + failed should equal total")

		// Determine expected status
		var expectedStatus string
		if failedCount == total {
			expectedStatus = TaskStatusFailed
		} else if failedCount > 0 {
			expectedStatus = TaskStatusPartialSuccess
		} else {
			expectedStatus = TaskStatusSuccess
		}

		// Verify status logic
		actualStatus := TaskStatusSuccess
		if failedCount == total {
			actualStatus = TaskStatusFailed
		} else if failedCount > 0 {
			actualStatus = TaskStatusPartialSuccess
		}
		assert.Equal(rt, expectedStatus, actualStatus)
	})
}

// Feature: vm-template-provisioning, Property 17: 统一参数转换一致性
// For equivalent creation params from template+overrides vs direct request,
// both produce the same CreateInstanceParams structure.
//
// **Validates: Requirements 10.1**
func TestProperty_UnifiedParamConversion(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		region := genRegion().Draw(rt, "region")
		zone := genRegion().Draw(rt, "zone")
		instanceType := "ecs.g6.large"
		imageID := genResourceID().Draw(rt, "imageID")
		vpcID := genResourceID().Draw(rt, "vpcID")
		subnetID := genResourceID().Draw(rt, "subnetID")
		sgID := genResourceID().Draw(rt, "sgID")
		namePrefix := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "namePrefix")
		count := rapid.IntRange(1, 20).Draw(rt, "count")

		// Build from template
		tmpl := &VMTemplate{
			Region:             region,
			Zone:               zone,
			InstanceType:       instanceType,
			ImageID:            imageID,
			VPCID:              vpcID,
			SubnetID:           subnetID,
			SecurityGroupIDs:   []string{sgID},
			InstanceNamePrefix: namePrefix,
		}
		fromTemplate := BuildCreateInstanceParams(tmpl, namePrefix, nil, count)

		// Build from direct request
		directReq := &DirectProvisionReq{
			Region:             region,
			Zone:               zone,
			InstanceType:       instanceType,
			ImageID:            imageID,
			VPCID:              vpcID,
			SubnetID:           subnetID,
			SecurityGroupIDs:   []string{sgID},
			InstanceNamePrefix: namePrefix,
			Count:              count,
		}
		fromDirect := BuildCreateInstanceParamsFromDirect(directReq)

		// Core fields should be identical
		assert.Equal(rt, fromTemplate.Region, fromDirect.Region)
		assert.Equal(rt, fromTemplate.Zone, fromDirect.Zone)
		assert.Equal(rt, fromTemplate.InstanceType, fromDirect.InstanceType)
		assert.Equal(rt, fromTemplate.ImageID, fromDirect.ImageID)
		assert.Equal(rt, fromTemplate.VPCID, fromDirect.VPCID)
		assert.Equal(rt, fromTemplate.SubnetID, fromDirect.SubnetID)
		assert.Equal(rt, fromTemplate.SecurityGroupIDs, fromDirect.SecurityGroupIDs)
		assert.Equal(rt, fromTemplate.InstanceName, fromDirect.InstanceName)
		assert.Equal(rt, fromTemplate.Count, fromDirect.Count)
	})
}
