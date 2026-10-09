// @feature disk-ops-insight @api-functional
//
// Contract shared-disk-multi-account-metrics step-4-ops-card-aggregation: the
// ops-card aggregation ( derived from the deduped Top surface — the API-side
// contract ) counts the shared disk once, removed accounts' historical rows
// drop out of the current tenant scope, and busy_share idle-disk zeros count
// as real data with data_status=ok.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package shared_disk_multi_account_metrics

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 聚合统计基于去重后的代表行 — 共享盘只计一次,不因多账号
// 并存而重复计入盘数、均值或繁忙盘数(运营卡口径 = Top 去重面,前端派生)。
func TestStep4_OpsCard_DedupAggregation(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accA.ID, provider.Name, "dsk-normal", "cn-test-1")
	// 共享盘两账号行(66/33)+ 普通盘一行(50)
	seedSharedMetricT(t, h, accA.ID, provider.Name, disktest.Today(), 66.0, 150, 11.0)
	seedSharedMetricT(t, h, accB.ID, provider.Name, disktest.Today(), 33.0, 70, 5.0)
	seedT(t, h, accA.ID, provider.Name, "dsk-normal", disktest.Today(), 50.0, 100, 8.0)

	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7")
	require.Equal(t, 200, status)

	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	// 盘数:共享盘只计一次 + 普通盘 = 2(不因多账号并存变 3)
	require.Equal(t, 2, resp.Total, "运营卡磁盘数基于去重后代表行:共享盘只计一次")
	// 均值:代表行口径(共享盘今日代表行 66),不是跨账号平均 (66+33)/2
	for _, item := range resp.Items {
		if item.DiskID == sharedDiskID {
			require.InDelta(t, 66.0, *item.Average.UsagePercent, 1e-9,
				"共享盘均值按每日代表行计算,不跨账号双计")
			require.Equal(t, []int64{accA.ID, accB.ID}, item.AccountIDs)
		}
	}
}

// Outcome removed-account-excluded: 账号 B 已从租户移除纳管,其历史指标行仍留
// 在 ecam_disk_metric — 聚合口径基于当前租户可见账号集合;已移除账号的行不
// 进入当前租户聚合,不产生幽灵盘数。
func TestStep4_OpsCard_RemovedAccountExcluded(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	seedSharedMetricT(t, h, accA.ID, provider.Name, disktest.Today(), 66.0, 150, 11.0)
	// 账号 B 已移除纳管:账号不存在于租户 1,但其历史行仍在库
	seedSharedMetricT(t, h, 2, provider.Name, disktest.Today(), 33.0, 70, 5.0)

	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7")
	require.Equal(t, 200, status)

	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 1, resp.Total, "已移除账号的行不进入当前租户聚合(共享盘仍计 1)")
	require.Len(t, resp.Items, 1)
	require.Equal(t, []int64{accA.ID}, resp.Items[0].AccountIDs, "已移除账号不出现在账号列表")
	require.InDelta(t, 66.0, *resp.Items[0].Average.UsagePercent, 1e-9, "无幽灵盘数/虚增值")
}

// Outcome busy-share-zero-counted: usage_percent=0 且 usage_scope=busy_share
// (AWS 派生口径全闲盘)— 该行按正常真数据参与均值与繁忙盘判定,
// data_status=ok,不被当异常排除。
func TestStep4_OpsCard_BusyShareZeroCounted(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, "dsk-aws-idle", "cn-test-1")
	// busy_share 全闲盘 0 值行(写路径统一打 zero_exception,读取侧甄别)
	seedRowWithScope(t, h, accA.ID, provider.Name, "dsk-aws-idle", disktest.Today(), 0, 5, 0.5, types.DiskUsageScopeBusyShare)
	// 对照组:口径缺失 0 行(usage_scope 为空)
	seedRowWithScope(t, h, accA.ID, provider.Name, "dsk-noscope-zero", disktest.Today(), 0, 0, 0, "")

	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7")
	require.Equal(t, 200, status)

	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Len(t, resp.Items, 2)
	for _, item := range resp.Items {
		if item.DiskID == "dsk-aws-idle" {
			require.Equal(t, service.DiskDataStatusOK, item.DataStatus,
				"busy_share 合法闲盘 0 是真数据,data_status=ok")
			// busy_share 0 参与均值(不被排除):均值即其自身 0
			require.NotNil(t, item.Average.UsagePercent)
			require.InDelta(t, 0, *item.Average.UsagePercent, 1e-9)
		} else {
			require.Equal(t, service.DiskDataStatusZeroException, item.DataStatus,
				"口径缺失 0(usage_scope 为空)仍是异常行")
		}
	}
}
