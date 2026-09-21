// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-daily-collection step-1-day-gate-claim: the disk key
// of the persistent daily gate — claim once per day ( concurrent racers lose
// silently ), first deploy without any record claims as initial without
// backfilling history, and an already-claimed day skips silently.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 当日 disk 键未被认领 — 恰好一个执行器认领胜出并提交
// disk:collect_metrics(days=2);scheduler_state disk 键 last_date 持久化推进
// 为今日;其余并发认领者静默跳过。
func TestStep1_DayGateClaim_Success(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	// 既有 nas/oss 日闸键已认领今日(fixture: 多资源并存,不干扰 disk 键)
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceOSS} {
		_, err := h.GateStore.TryClaimDaily(nil, res, disktest.Today())
		require.NoError(t, err)
	}

	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "当日 disk 键应认领成功")
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk))
	// 并发认领者(新 gate 实例)静默跳过
	require.False(t, submitDiskDailyCollect(t, h, newGate(h), queue))
	requireSubmittedDiskDailyTasks(t, h, 1)
}

// Outcome first-claim-transition: scheduler_state 尚无 disk 键记录(特性启用
// 后第一天)— 视为首次认领,认领成功触发一次当日提交;不回溯补采启用日之前
// 的日期(启用日之前无指标行属预期)。
func TestStep1_DayGateClaim_FirstClaimTransition(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceDisk), "前置:disk 键无任何记录")
	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "首次认领应成功")
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk),
		"首次认领后应新建 disk 键记录并标记当日已认领")
	require.Equal(t, 0, h.MetricDAO.Count(), "首次认领不回溯补采历史指标")
}

// Outcome already-claimed-skip: 当日 disk 键已被认领 — 认领返回未胜出静默
// 跳过;不创建采集任务,不产生任何指标写入;scheduler_state 当日记录不变。
func TestStep1_DayGateClaim_AlreadyClaimedSkip(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitDiskDailyCollect(t, h, gate, queue))
	require.False(t, submitDiskDailyCollect(t, h, gate, queue), "同日再次认领应跳过")
	require.False(t, submitDiskDailyCollect(t, h, newGate(h), queue), "另一执行器同日认领应跳过")
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk), "当日记录保持不变")
}
