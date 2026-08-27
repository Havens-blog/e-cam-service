package scheduler

import (
	"reflect"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

func TestBuildSyncParams_DefaultIncludesDNS(t *testing.T) {
	// SupportedAssetTypes 为空（默认配置）-> 使用默认清单，必须含 dns
	account := &domain.CloudAccount{
		ID:       7,
		Provider: domain.CloudProviderAliyun,
		TenantID: 1,
	}

	params := buildSyncParams(account)

	types, ok := params["asset_types"].([]string)
	if !ok {
		t.Fatalf("asset_types 类型不是 []string: %T", params["asset_types"])
	}

	found := false
	for _, at := range types {
		if at == "dns" {
			found = true
		}
	}
	if !found {
		t.Errorf("默认资产类型清单缺少 dns，实际: %v", types)
	}

	if params["auto_sync"] != true {
		t.Errorf("auto_sync 应为 true")
	}
	if params["account_id"] != int64(7) {
		t.Errorf("account_id 不匹配: %v", params["account_id"])
	}
}

func TestBuildSyncParams_ExplicitConfigPreserved(t *testing.T) {
	// 显式配置 SupportedAssetTypes（不含 dns）-> 任务参数与配置完全一致，不强制加入 dns
	explicit := []string{"ecs", "rds"}
	account := &domain.CloudAccount{
		ID:       8,
		Provider: domain.CloudProviderTencent,
		TenantID: 2,
	}
	account.Config.SupportedAssetTypes = explicit

	params := buildSyncParams(account)

	got, ok := params["asset_types"].([]string)
	if !ok {
		t.Fatalf("asset_types 类型不是 []string: %T", params["asset_types"])
	}
	if !reflect.DeepEqual(got, explicit) {
		t.Errorf("显式配置被覆盖: 期望 %v, 实际 %v", explicit, got)
	}
	for _, at := range got {
		if at == "dns" {
			t.Errorf("显式配置不含 dns 时不应强制加入: %v", got)
		}
	}
}

func TestDefaultSyncAssetTypes_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, at := range domain.DefaultSyncAssetTypes {
		if seen[at] {
			t.Errorf("默认清单存在重复项: %s", at)
		}
		seen[at] = true
	}
}
