// @feature disk-ops-insight @api-functional
//
// Contract disk-day-gate-resilience step-2-gate-commit: committing today's
// gate after a successful collect stops further same-day triggers, read
// failures enter the >=5min backoff window instead of hot-looping the store,
// and a queue Submit failure after a persisted claim never rolls the claim
// back.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_day_gate_resilience

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 采集完成后提交日闸 — 当日记录为已完成/已认领,当日后续
// 分钟级调度检查全部跳过,不再重复采集。
func TestStep2_GateCommit_Success(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	// 认领 + 采集(执行器跑一轮全量采集作为提交前置)+ 提交当日完成态
	require.True(t, submitDiskDailyCollect(t, h, gate, queue))
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 当日后续分钟级调度检查:全部跳过
	for i := 0; i < 3; i++ {
		require.False(t, submitDiskDailyCollect(t, h, gate, queue), "当日已认领/已提交,后续调度检查应跳过")
	}
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk))
}

// Outcome read-failure-backoff: 读取 scheduler_state 状态时存储不可用 —
// 进入 ≥5 分钟退避窗口,不热循环刷存储(读失败注入期间反复尝试只打存储一次);
// 恢复(新实例)后按正常语义继续认领;无部分写入,当日事实不被破坏。
func TestStep2_GateCommit_ReadFailureBackoff(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)
	injected := errors.New("injected: scheduler_state read failure")

	h.GateStore.FailGet(scheduler.GateResourceDisk, injected)
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
	require.Error(t, err)
	require.False(t, claimed)

	// 退避窗口内同实例静默跳过((false,nil)):即便存储已恢复也不重读不认领,
	// 不热循环刷存储(窗口短路发生在读取之前)
	h.GateStore.FailGet(scheduler.GateResourceDisk, nil)
	claimed, err = gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
	require.NoError(t, err, "退避窗口内应静默跳过而非报错")
	require.False(t, claimed, "退避窗口内不得认领")

	// 无部分写入:当日认领事实未被破坏;恢复(新实例,无退避状态)后正常认领
	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceDisk))
	requireSubmittedDiskDailyTasks(t, h, 0)
	require.True(t, submitDiskDailyCollect(t, h, newGate(h), queue), "恢复后应按正常语义认领成功")
	requireSubmittedDiskDailyTasks(t, h, 1)
}

// Outcome submit-failure-no-rollback: 当日认领已持久化但任务队列 Submit 失败
// (队列打满/关闭)— 记 ERROR 留痕,不回滚当日认领,今日不再重试(避免与
// 多副本认领竞争);当日认领保持已认领。
func TestStep2_GateCommit_SubmitFailureNoRollback(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h) // BufferSize=1,无 worker 消费

	// 先占满队列缓冲(注入「队列打满」形态)
	require.NoError(t, queue.Submit(&taskx.Task{
		ID: "diskjt-buffer-filler", Type: executor.TaskTypeDiskCollectMetrics, Status: "pending",
	}))

	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
	require.NoError(t, err)
	require.True(t, claimed, "认领应成功(持久化先行)")
	submitErr := queue.Submit(&taskx.Task{
		ID: "diskjt-daily-" + disktest.Today(), Type: executor.TaskTypeDiskCollectMetrics,
		Status: "pending", Params: map[string]any{"days": 2}, CreatedBy: "diskjt-daily",
	})
	require.Error(t, submitErr, "队列打满时 Submit 应失败")

	// 不回滚:当日认领保持已认领,今日不再重试
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk),
		"提交失败不得回滚当日认领")
	require.False(t, submitDiskDailyCollect(t, h, newGate(h), queue),
		"今日不再重试:竞争实例同日认领仍应失败")
}
