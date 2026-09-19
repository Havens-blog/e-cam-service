// @feature nas-ops-insight @api-functional
//
// Journey smoke: vendor-failure-observability happy path — a best-effort
// vendor failing alongside a healthy mandatory vendor keeps the flow green,
// the failure lands in Result.failures, zero-capacity rows stay visible, and
// recovery clears the warning signal.
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

func TestVendorFailureObservability_FullJourneySmoke(t *testing.T) {
	h, mandatory := newJourneyHarness()
	bestEffort := h.NewProvider(true)

	// Step 1: 弱厂商失败返回空,不阻塞必达厂商采集
	h.SeedAccount(1, mandatory)
	h.SeedAccount(2, bestEffort)
	h.SeedInstance(1, mandatory, "fs-ali", "必达盘", "cn-hangzhou")
	h.SeedInstance(1, mandatory, "fs-zero", "零容量盘", "cn-beijing")
	h.SeedInstance(2, bestEffort, "fs-tenc", "尽力盘", "ap-shanghai")
	mandatory.Querier.SetMetrics(
		nastest.Metric("fs-ali", "必达盘", nastest.Today(), 100, 10),
		nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))
	bestEffort.Querier.Fail(errors.New("injected: vendor API outage"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "尽力而为厂商失败不阻塞全流程")

	// Step 2: 失败计数与末次错误进入任务 Result
	failures := nastest.ResultFailures(t, task)
	entry, found := nastest.FailureFor(failures, bestEffort.Name)
	require.True(t, found)
	require.Equal(t, int64(2), entry.AccountID)
	require.Equal(t, 2, h.MetricDAO.Count(), "失败厂商不落库,必达厂商两行(含 zero_exception)")

	// 零容量行可见(zero_exception 落库不吞)
	row, ok := h.MetricDAO.Row(1, "fs-zero", nastest.Today())
	require.True(t, ok)
	require.Equal(t, "zero_exception", row.QcStatus)

	// Step 3: 恢复后空态回到正常数据态
	bestEffort.Querier.Recover()
	bestEffort.Querier.SetMetrics(nastest.Metric("fs-tenc", "尽力盘", nastest.Today(), 50, 5))
	recovered, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, nastest.ResultFailures(t, recovered), "警示随失败计数清零解除")

	trend, err := h.NewQueryService().GetFsMetrics(context.Background(), h.TenantID, 2, "fs-tenc", 7)
	require.NoError(t, err)
	today := trend.Days[len(trend.Days)-1]
	require.Equal(t, service.NASDataStatusOK, today.DataStatus)
}
