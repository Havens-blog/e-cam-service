// @feature disk-ops-insight @api-functional
//
// Contract shared-disk-multi-account-metrics step-1-shared-disk-persist: the
// shared disk persists as two independent rows ( one per account ), an
// account-side failure never touches the other account's row and stays
// observable in Result.failures, and empty-date rows are skipped before they
// could not locate a unique key.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package shared_disk_multi_account_metrics

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 采集执行器分别对账号 A 与账号 B 采集落库 — ecam_disk_metric
// 同日存在两行,各行独立成立,互不覆盖(唯一键含 account_id 隔离)。
func TestStep1_SharedDiskPersist_TwoIndependentRows(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	// 两账号各自采集结果不同(同盘不同账号口径)
	provider.QuerierFor(accA.ID).SetMetrics(disktest.Metric(sharedDiskID, disktest.Today(), 66.0, 150, 11.0))
	provider.QuerierFor(accB.ID).SetMetrics(disktest.Metric(sharedDiskID, disktest.Today(), 33.0, 70, 5.0))

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	rowA, okA := h.MetricDAO.Row(accA.ID, sharedDiskID, disktest.Today())
	rowB, okB := h.MetricDAO.Row(accB.ID, sharedDiskID, disktest.Today())
	require.True(t, okA, "账号 A 行应独立成立")
	require.True(t, okB, "账号 B 行应独立成立")
	require.InDelta(t, 66.0, rowA.UsagePercent, 1e-9)
	require.InDelta(t, 33.0, rowB.UsagePercent, 1e-9, "两行各自独立,互不覆盖")
	require.Equal(t, accA.ID, rowA.AccountID)
	require.Equal(t, accB.ID, rowB.AccountID)
	require.Equal(t, 2, h.MetricDAO.Count(), "同日恰两行(每账号一行)")
}

// Outcome independent-account-failure: 账号 A 采集成功、账号 B 采集失败 —
// 账号 A 的成功行不被账号 B 的失败影响;账号 B 失败计入 Result.failures
// (provider/account_id/error_count/last_error) 可观测。
func TestStep1_SharedDiskPersist_IndependentAccountFailure(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	provider.QuerierFor(accA.ID).SetMetrics(disktest.Metric(sharedDiskID, disktest.Today(), 66.0, 150, 11.0))
	provider.QuerierFor(accB.ID).Fail(errors.New("injected: account B vendor failure"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单账号失败不得让任务整体失败")

	// 账号 A 行完好
	rowA, ok := h.MetricDAO.Row(accA.ID, sharedDiskID, disktest.Today())
	require.True(t, ok, "账号 A 成功行不受账号 B 失败影响")
	require.InDelta(t, 66.0, rowA.UsagePercent, 1e-9)
	// 账号 B 无新行,失败可观测
	_, ok = h.MetricDAO.Row(accB.ID, sharedDiskID, disktest.Today())
	require.False(t, ok, "账号 B 失败无新行")
	failures := nastest.ResultFailures(t, task)
	require.NotEmpty(t, failures)
	var bFailure *nastest.ProviderFailure
	for i := range failures {
		if failures[i].AccountID == accB.ID {
			bFailure = &failures[i]
		}
	}
	require.NotNil(t, bFailure, "账号 B 失败应入 failures")
	require.GreaterOrEqual(t, bFailure.ErrorCount, 1)
	require.NotEmpty(t, bFailure.LastError)
	require.Equal(t, 1, task.Result["failed_disks"])
}

// Outcome missing-date-row-skip: 适配器返回的某指标行 date 为空 — 该行被跳过
// 不入批,不产生无法定位唯一键的脏行;其余有效行正常落库。
func TestStep1_SharedDiskPersist_MissingDateRowSkip(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccountNamed(1, provider.Name)
	h.SeedDisk(acc.ID, provider.Name, sharedDiskID, "cn-test-1")
	provider.QuerierFor(acc.ID).SetMetrics(
		disktest.Metric(sharedDiskID, "", 50.0, 100, 8.0), // 空日期行(脏数据形态)
		disktest.Metric(sharedDiskID, disktest.Today(), 66.0, 150, 11.0),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 仅有效行落库;空日期行不入批(metrics_total 只含有效行)
	require.Equal(t, 1, task.Result["metrics_total"], "空日期行应被跳过,不入批")
	require.Equal(t, 1, h.MetricDAO.Count(), "无空日期脏行")
	_, ok := h.MetricDAO.Row(acc.ID, sharedDiskID, disktest.Today())
	require.True(t, ok, "有效行应正常落库")
	_ = types.DiskMetricQcOK
}
