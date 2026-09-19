// @feature nas-ops-insight @api-functional
//
// Contract step-3-empty-state-distinction: 前端空态区分无数据与采集失败 — the
// three states ( truly empty / collect failed / zero_exception ) are
// distinguishable on the api surface: empty Result.failures + missing days =
// truly empty, non-empty failures = collect-failed warning, qc zero_exception
// + data_status mapping = zero anomaly; recovery clears the warning because
// the latest run's Result is authoritative. Display rendering itself is
// frontend logic ( see doc.go scope note ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package vendor_failure_observability

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 失败厂商实例窗口内无指标行且最近任务 Result 无失败
// (真实无数据形态)— 空态呈现「无数据」分支:全窗口 missing 日 + 空 failures,
// 采集异常警示的 api 信号不存在。
func TestStep3_EmptyStateDistinction_TrulyEmpty(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	// 采集任务成功执行且厂商返回空结果(真实无数据)
	collectRun, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 空 failures(真实无数据的 api 信号)
	require.Empty(t, nastest.ResultFailures(t, collectRun))

	// 读取侧:窗口内全为 missing 日(不填充假值),非 zero_exception
	trend, err := h.NewQueryService().GetFsMetrics(context.Background(), h.TenantID, account.ID, "fs-tenc", 30)
	require.NoError(t, err)
	for _, day := range trend.Days {
		require.Equal(t, service.NASDataStatusMissing, day.DataStatus)
		require.Nil(t, day.Capacity)
	}
}

// Outcome collect-failure-warning-shown: 最近采集任务 Result 失败计数大于 0
// (采集失败形态)且窗口内指标行缺失 — api 面以非空 failures 表达警示态,
// 与真实无数据(空 failures)互斥可分辨。
func TestStep3_EmptyStateDistinction_CollectFailureWarningShown(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-tenc", "尽力盘", "ap-shanghai")
	provider.Querier.Fail(errors.New("injected: vendor API outage"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 警示态信号:failures 非空且 error_count>0(与真实无数据互斥)
	failures := nastest.ResultFailures(t, task)
	require.NotEmpty(t, failures)
	entry, _ := nastest.FailureFor(failures, provider.Name)
	require.Greater(t, entry.ErrorCount, 0)
}

// Outcome warning-cleared-after-recovery: 失败厂商次日恢复成功写库后,最近
// 一次任务 Result 失败计数清零,警示随之解除不残留。
func TestStep3_EmptyStateDistinction_WarningClearedAfterRecovery(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-tenc", "尽力盘", "ap-shanghai")

	// 首日:采集失败 → 警示态
	provider.Querier.Fail(errors.New("injected: vendor API outage"))
	failedRun, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.NotEmpty(t, nastest.ResultFailures(t, failedRun))

	// 次日恢复:窗口内出现成功落库行,最近一次任务 Result 失败计数为 0
	provider.Querier.Recover()
	provider.Querier.SetMetrics(nastest.Metric("fs-tenc", "尽力盘", nastest.Today(), 50, 5))
	recoveredRun, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, nastest.ResultFailures(t, recoveredRun), "警示随失败计数清零解除,不残留过期警示")
	require.Equal(t, 1, nastest.ResultField(t, recoveredRun, "metrics_total").(int))

	// 数据态恢复正常(ok),不再是警示/missing
	trend, err := h.NewQueryService().GetFsMetrics(context.Background(), h.TenantID, 1, "fs-tenc", 30)
	require.NoError(t, err)
	today := trend.Days[len(trend.Days)-1]
	require.Equal(t, service.NASDataStatusOK, today.DataStatus)
	require.NotNil(t, today.Utilization)
}
