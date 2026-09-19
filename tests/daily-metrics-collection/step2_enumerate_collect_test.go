// @feature nas-ops-insight @api-functional
//
// Contract step-2-enumerate-collect: 按活跃账号遍历实例采集 — the executor
// walks active accounts' local NAS instances over [yesterday, today] with the
// instance region passed through, isolates vendor failures to the Result
// failures array, and records instance-less accounts as accounts_without_nas.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 两个 NAS 实例在 [昨日, 今日] 各产出两组日值,任务成功且
// metrics_total 大于 0;厂商查询入参携带实例 region 与完整采集区间。
func TestStep2_EnumerateCollect_ProducesYesterdayAndToday(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	h.SeedInstance(1, provider, "fs-b", "共享盘B", "cn-beijing")
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 90, 9),
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 100, 10),
		nastest.Metric("fs-b", "共享盘B", nastest.DayOffset(-1), 45, 4.5),
		nastest.Metric("fs-b", "共享盘B", nastest.Today(), 50, 5),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Greater(t, nastest.ResultField(t, task, "metrics_total").(int), 0)
	require.Equal(t, 4, h.MetricDAO.Count(), "两实例 × [昨日,今日] 应产出四行日指标")

	// 每实例各一次厂商查询,区间固定 [昨日, 今日],region 透传
	calls := provider.Querier.Calls()
	require.Len(t, calls, 2)
	regions := map[string]string{}
	for _, call := range calls {
		require.Equal(t, nastest.DayOffset(-1), call.Start)
		require.Equal(t, nastest.Today(), call.End)
		regions[call.FsID] = call.Region
	}
	require.Equal(t, "cn-hangzhou", regions["fs-a"])
	require.Equal(t, "cn-beijing", regions["fs-b"])
}

// Outcome vendor-failure-isolated: 一家厂商调用失败只影响自身 — 失败进 Result
// 的 failures(计数与末次错误),其余厂商照常产出完整指标,任务整体不失败。
func TestStep2_EnumerateCollect_VendorFailureIsolated(t *testing.T) {
	h, okProvider := newJourneyHarness()
	badProvider := h.NewProvider(true)

	h.SeedAccount(1, okProvider)
	h.SeedAccount(2, badProvider)
	h.SeedInstance(1, okProvider, "fs-ok", "健康盘", "cn-hangzhou")
	h.SeedInstance(2, badProvider, "fs-bad", "故障盘", "cn-shanghai")
	okProvider.Querier.SetMetrics(
		nastest.Metric("fs-ok", "健康盘", nastest.DayOffset(-1), 90, 9),
		nastest.Metric("fs-ok", "健康盘", nastest.Today(), 100, 10),
	)
	badProvider.Querier.Fail(errors.New("injected: vendor monitor API down"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单厂商失败不得使任务整体失败")

	failures := nastest.ResultFailures(t, task)
	entry, found := nastest.FailureFor(failures, badProvider.Name)
	require.True(t, found, "失败厂商应出现在 Result.failures")
	require.GreaterOrEqual(t, entry.ErrorCount, 1)
	require.Contains(t, entry.LastError, "vendor monitor API down")
	require.Equal(t, int64(2), entry.AccountID)

	// 其余厂商照常产出昨日与今日两组指标
	require.Equal(t, 2, h.MetricDAO.Count())
	require.True(t, hasRow(h, 1, "fs-ok", nastest.Today()))
	require.True(t, hasRow(h, 1, "fs-ok", nastest.DayOffset(-1)))
	// 失败厂商实例不落库指标行
	require.False(t, hasRow(h, 2, "fs-bad", nastest.Today()))
}

// Outcome account-without-nas-recorded: 无 NAS 实例的账号不调厂商 API,记入
// Result 的 accounts_without_nas,任务整体仍成功。
func TestStep2_EnumerateCollect_AccountWithoutNASRecorded(t *testing.T) {
	h, provider := newJourneyHarness()
	withNAS := h.SeedAccount(1, provider)
	withoutNAS := h.SeedAccount(2, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 100, 10),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	without := nastest.ResultStrings(t, task, "accounts_without_nas")
	require.Contains(t, without, withoutNAS.Name, "无实例账号应可观测地记入 accounts_without_nas")
	require.NotContains(t, without, withNAS.Name)

	// 无实例账号不产生任何厂商调用与指标行
	for _, call := range provider.Querier.Calls() {
		require.Equal(t, "fs-a", call.FsID, "只应枚举有实例账号的实例")
	}
	require.False(t, hasRow(h, 2, "fs-a", nastest.Today()))
}

// hasRow 行存在断言辅助。
func hasRow(h *nastest.Harness, accountID int64, fsID, date string) bool {
	_, ok := h.MetricDAO.Row(accountID, fsID, date)
	return ok
}
