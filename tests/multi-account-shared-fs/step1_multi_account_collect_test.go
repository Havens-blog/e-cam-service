// @feature nas-ops-insight @api-functional
//
// Contract step-1-multi-account-collect: 三账号同 fs 并发采集 — three active
// accounts each collect the same physical fs through their own adapter,
// account failures stay isolated in Result.failures, and capacity=0 rows are
// kept per account as zero_exception.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_fs

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 三个账号以各自身份采集同一 fs_id,各产出各自口径的
// capacity/used,任务整体成功。
func TestStep1_MultiAccountCollect_ThreeAccountsOwnViews(t *testing.T) {
	h, provider := newJourneyHarness()
	seedSharedFSCollect(t, h, provider, map[int64]float64{1: 100, 2: 200, 3: 300})

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 3, nastest.ResultField(t, task, "metrics_total").(int), "三账号各落一行当日指标")
	require.Empty(t, nastest.ResultFailures(t, task))

	// 各账号行值各归其主
	nastest.RequireNoCrossAccountLoss(t, h.MetricDAO, SharedFS, nastest.Today(),
		map[int64]float64{1: 100, 2: 200, 3: 300})
}

// Outcome account-failure-isolated: 三账号中任一账号适配器调用失败仅返回
// 自身空结果,其余账号照常产出,失败进入 Result 的 failures(账号维度)。
func TestStep1_MultiAccountCollect_AccountFailureIsolated(t *testing.T) {
	h, provider := newJourneyHarness()
	seedSharedFSCollect(t, h, provider, map[int64]float64{1: 100, 2: 200, 3: 300})
	provider.QuerierFor(2).Fail(errors.New("injected: account-2 monitor API down"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单账号失败不得使任务整体失败")

	entry, found := nastest.FailureFor(nastest.ResultFailures(t, task), provider.Name)
	require.True(t, found, "失败账号进入 Result.failures")
	require.Equal(t, int64(2), entry.AccountID)
	require.Contains(t, entry.LastError, "account-2")

	// 失败账号不产出指标,其余两账号结果完整
	require.False(t, hasRow(h, 2))
	require.True(t, hasRow(h, 1))
	require.True(t, hasRow(h, 3))
}

// Outcome zero-capacity-rows-kept: 某账号厂商返回 capacity=0,其余账号正常 —
// 零值行按该账号独立落库(zero_exception),同 fs 同日仍三行。
func TestStep1_MultiAccountCollect_ZeroCapacityRowsKept(t *testing.T) {
	h, provider := newJourneyHarness()
	seedSharedFSCollect(t, h, provider, map[int64]float64{1: 100, 2: 0, 3: 300})

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 3, nastest.ResultField(t, task, "metrics_total").(int), "零容量行照常计入")

	// 同 fs 同日三行,其一 zero_exception,互不覆盖
	row2, ok := h.MetricDAO.Row(2, SharedFS, nastest.Today())
	require.True(t, ok, "capacity=0 行按账号独立保留")
	require.Equal(t, "zero_exception", row2.QcStatus)
	nastest.RequireNoCrossAccountLoss(t, h.MetricDAO, SharedFS, nastest.Today(),
		map[int64]float64{1: 100, 2: 0, 3: 300})
}

// hasRow 行存在断言辅助。
func hasRow(h *nastest.Harness, accountID int64) bool {
	_, ok := h.MetricDAO.Row(accountID, SharedFS, nastest.Today())
	return ok
}
