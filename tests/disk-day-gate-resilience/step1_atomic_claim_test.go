// @feature disk-ops-insight @api-functional
//
// Contract disk-day-gate-resilience step-1-atomic-claim: the disk key claims
// each day exactly once via an atomic conditional write, concurrent claims
// produce exactly one winner, and write failures back off with an escalation
// alert instead of silently dropping the day.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_day_gate_resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 当日 disk 键未被认领时,执行器原子认领胜出并提交
// disk:collect_metrics(days=2);scheduler_state disk 键 last_date 持久化推进为今日。
func TestStep1_AtomicClaim_Success(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "当日未认领时 disk 键应认领成功")
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk),
		"认领成功后 scheduler_state 的 disk 键 last_date 应持久化推进为今日")
}

// Outcome already-claimed-skip: 当日 disk 键已被认领 — 再次触发静默跳过,
// 不重复采集、不创建任务、当日记录不变。
func TestStep1_AtomicClaim_AlreadyClaimedSkip(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitDiskDailyCollect(t, h, gate, queue))
	require.False(t, submitDiskDailyCollect(t, h, gate, queue), "当日已认领,再次认领应跳过")
	require.False(t, submitDiskDailyCollect(t, h, newGate(h), queue), "另一执行器(新 gate 实例)同日认领同样跳过")
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk), "当日记录不变")
	require.Empty(t, h.GateAlerter.Calls(), "正常竞争语义不触发告警")
}

// Outcome concurrent-single-winner: 多执行器并发发起当日 disk 键认领 —
// 原子认领保证恰好一个胜出,其余全部静默跳过;不存在双执行,也不存在全部
// 跳过(死锁);仅胜出方提交采集任务。
func TestStep1_AtomicClaim_ConcurrentSingleWinner(t *testing.T) {
	h := newJourneyHarness()
	store := h.GateStore
	queue := newTaskQueue(h)

	const racers = 16
	var winners atomic.Int64
	var mu sync.Mutex
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个 racer 一颗全新 gate 实例,共享同一持久化存储(多副本形态)
			gate := scheduler.NewPersistentDailyGate(store, h.GateAlerter, newTestLogger())
			<-start
			claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
			require.NoError(t, err)
			if !claimed {
				return
			}
			mu.Lock()
			task := &taskx.Task{
				ID:        "diskjt-race-" + disktest.Today(),
				Type:      executor.TaskTypeDiskCollectMetrics,
				Status:    "pending",
				Params:    map[string]any{"days": 2},
				CreatedBy: "diskjt-race",
			}
			require.NoError(t, queue.Submit(task))
			mu.Unlock()
			winners.Add(1)
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int64(1), winners.Load(), "并发认领必须恰好一个胜出")
	requireSubmittedDiskDailyTasks(t, h, 1)
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk))
	require.Empty(t, h.GateAlerter.Calls(), "正常竞争不产生告警")
}

// Outcome write-failure-backoff-alert: 认领写连续失败 — 指数退避重试耗尽后
// 返回错误并经 SchedulerGateAlerter 升级告警(resource_type=disk,
// operation=write);不静默丢失当日认领,未认领成功不提交采集任务;恢复
// (新实例加载同一持久化存储)后当日正常认领一次。
func TestStep1_AtomicClaim_WriteFailureBackoffAlert(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)
	injected := errors.New("injected: scheduler_state write failure")

	h.GateStore.FailClaim(scheduler.GateResourceDisk, 4, injected)
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
	require.Error(t, err, "重试耗尽应返回错误而非静默跳过")
	require.False(t, claimed)
	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceDisk), "写失败期间 last_date 不得推进")

	alerts := h.GateAlerter.Calls()
	require.NotEmpty(t, alerts, "重试耗尽必须升级告警,不得仅记日志")
	last := alerts[len(alerts)-1]
	require.Equal(t, scheduler.GateResourceDisk, last.ResourceType)
	require.Equal(t, "write", last.Operation)
	require.ErrorIs(t, last.Err, injected)

	// 未认领成功不采集:任务队列为空;跨轮退避窗口内同实例静默跳过
	requireSubmittedDiskDailyTasks(t, h, 0)
	require.False(t, submitDiskDailyCollect(t, h, gate, queue), "退避窗口内同实例应静默跳过")
	requireSubmittedDiskDailyTasks(t, h, 0)
	// 恢复(新实例)后当日正常认领一次并提交
	require.True(t, submitDiskDailyCollect(t, h, newGate(h), queue), "恢复后应正常认领一次")
	requireSubmittedDiskDailyTasks(t, h, 1)
}
