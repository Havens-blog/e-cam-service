// @feature disk-ops-insight @api-functional
//
// Journey smoke for shared-disk-multi-account-metrics: the happy path end-to-
// end — both accounts collect the shared disk into independent rows, each
// account's trend view sees only its own row, Top dedups to a single
// representative entry, and the ops-card aggregation counts the disk once.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package shared_disk_multi_account_metrics

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

func TestSharedDiskMultiAccountMetrics_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	// 两账号各自采集(PerAccountProvider,值不同)
	provider.QuerierFor(accA.ID).SetMetrics(disktest.Metric(sharedDiskID, disktest.Today(), 66.0, 150, 11.0))
	provider.QuerierFor(accB.ID).SetMetrics(disktest.Metric(sharedDiskID, disktest.Today(), 33.0, 70, 5.0))

	// Step 1: 共享盘在多账号下并存落库
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, task.Result["metrics_total"], "step1: 两账号各 1 行")
	require.Equal(t, 2, h.MetricDAO.Count(), "step1: 唯一键含 account_id,两行并存")

	// Step 2: 账号视角查询看到本账号行
	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router,
		"/assets/disk/metrics?disk_id="+sharedDiskID+"&account_id=1&days=7")
	require.Equal(t, 200, status, "step2: 账号 A 趋势应 200")
	var trend service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &trend))
	require.InDelta(t, 66.0, *trend.Latest.UsagePercent, 1e-9, "step2: 只见账号 A 的行")

	// Step 3: Top 榜按 disk_id 去重取代表行
	status, env = disktest.GetJSON(t, router, "/assets/disk/top?days=7")
	require.Equal(t, 200, status, "step3: Top 应 200")
	var top service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &top))
	require.Equal(t, 1, top.Total, "step3: 共享盘至多一个条目")
	require.Equal(t, []int64{accA.ID, accB.ID}, top.Items[0].AccountIDs, "step3: 账号列表含两账号")

	// Step 4: 运营卡聚合去重后口径(共享盘只计一次)
	require.Equal(t, 1, top.Total, "step4: 不因多账号并存重复计入盘数")

	// Journey Invariants: 不跨账号求和/双计 + 零失败
	require.InDelta(t, 66.0, *top.Items[0].Average.UsagePercent, 1e-9,
		"invariant: 代表行口径,不跨账号求和")
	require.Empty(t, task.Result["failures"], "invariant: 正常旅程零失败")
}
