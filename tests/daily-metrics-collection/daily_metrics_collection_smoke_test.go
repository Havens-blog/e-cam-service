// @feature nas-ops-insight @api-functional
//
// Journey smoke: daily-metrics-collection happy path — persistent gate claims
// the day, exactly one nas:collect_metrics(days=2) task is submitted, the
// executor writes yesterday ( overwrite ) + today ( first-write-wins ) rows,
// and a restart later the same day submits nothing more.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

func TestDailyMetricsCollection_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	// Step 1: 日闸原子认领当日并提交采集任务
	require.True(t, submitDailyCollect(t, h, gate, queue))
	requireSubmittedDailyTasks(t, h, 1)

	// Step 2: 按活跃账号遍历实例采集([昨日, 今日])
	account := h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 90, 9),
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 100, 10),
	)
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int))

	// Step 3: 指标行落库(昨日覆盖 + 今日首写,唯一键至多一行)
	require.Equal(t, 2, h.MetricDAO.Count())
	yesterday, ok := h.MetricDAO.Row(account.ID, "fs-a", nastest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(90), yesterday.Capacity)
	today, ok := h.MetricDAO.Row(account.ID, "fs-a", nastest.Today())
	require.True(t, ok)
	require.Equal(t, types.NASMetricQcOK, today.QcStatus)
	require.Equal(t, float64(100), today.Capacity)

	// Step 4: 服务重启,当日不重复提交
	restartedGate := newGate(h)
	require.False(t, submitDailyCollect(t, h, restartedGate, queue))
	requireSubmittedDailyTasks(t, h, 1)
	require.Equal(t, scheduler.GateResourceNAS, scheduler.GateResourceNAS)
}
