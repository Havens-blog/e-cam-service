package executor

import (
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
)

// TestEnrichResourceGroupName 阿里云资源组名称补采：按 resource_group_id（或阿里云映射到
// project_id）反查名称写入 attributes.resource_group_name；未命中/无映射不动字段。
func TestEnrichResourceGroupName(t *testing.T) {
	e := &SyncAssetsExecutor{
		resourceGroupNames: map[string]string{
			"rg-1": "研发资源组",
			"rg-2": "测试资源组",
		},
	}

	t.Run("resource_group_id 命中", func(t *testing.T) {
		inst := camdomain.Instance{Attributes: map[string]any{"resource_group_id": "rg-1"}}
		e.enrichResourceGroupName(&inst)
		if got := inst.Attributes["resource_group_name"]; got != "研发资源组" {
			t.Fatalf("resource_group_name = %v, want 研发资源组", got)
		}
	})

	t.Run("阿里云 ECS 映射到 project_id 命中", func(t *testing.T) {
		inst := camdomain.Instance{Attributes: map[string]any{"project_id": "rg-2"}}
		e.enrichResourceGroupName(&inst)
		if got := inst.Attributes["resource_group_name"]; got != "测试资源组" {
			t.Fatalf("resource_group_name = %v, want 测试资源组", got)
		}
	})

	t.Run("resource_group_id 未命中回落 project_id", func(t *testing.T) {
		inst := camdomain.Instance{Attributes: map[string]any{"resource_group_id": "rg-x", "project_id": "rg-1"}}
		e.enrichResourceGroupName(&inst)
		if got := inst.Attributes["resource_group_name"]; got != "研发资源组" {
			t.Fatalf("resource_group_name = %v, want 研发资源组", got)
		}
	})

	t.Run("无匹配不改字段", func(t *testing.T) {
		inst := camdomain.Instance{Attributes: map[string]any{"resource_group_id": "unknown"}}
		e.enrichResourceGroupName(&inst)
		if _, ok := inst.Attributes["resource_group_name"]; ok {
			t.Fatalf("不应写入 resource_group_name: %v", inst.Attributes)
		}
	})

	t.Run("无映射时 no-op", func(t *testing.T) {
		inst := camdomain.Instance{Attributes: map[string]any{"resource_group_id": "rg-1"}}
		(&SyncAssetsExecutor{}).enrichResourceGroupName(&inst)
		if _, ok := inst.Attributes["resource_group_name"]; ok {
			t.Fatalf("无映射不应写入: %v", inst.Attributes)
		}
	})
}