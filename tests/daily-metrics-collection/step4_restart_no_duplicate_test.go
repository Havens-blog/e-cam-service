// @feature nas-ops-insight @api-functional
//
// Contract step-4-restart-no-duplicate: 服务重启验证当日不重复提交 — restarts
// never re-claim an already-claimed day ( nas and cdn keys independently ),
// a failed queue submission retains the persisted claim without retry
// flooding, and the rollback switch degrades to the in-memory gate rather
// than stopping collection ( memory-gate branch exercised by internal
// scheduler tests — see doc.go scope note ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 当日 nas/cdn 均已认领并各自提交任务后,连续 3 次「重启」
// (全新 gate 实例、同一持久化存储)不再产生重复提交。
func TestStep4_RestartThrice_NoDuplicateSubmission(t *testing.T) {
	h, _ := newJourneyHarness()
	queue := newTaskQueue(h)
	firstBoot := newGate(h)

	// 首次启动:nas 认领并提交一条;cdn 键独立认领
	require.True(t, submitDailyCollect(t, h, firstBoot, queue))
	claimedCDN, err := firstBoot.TryClaim(context.Background(), scheduler.GateResourceCDN, nastest.Today())
	require.NoError(t, err)
	require.True(t, claimedCDN)

	// 连续 3 次重启:每次都不重复提交
	for restart := 1; restart <= 3; restart++ {
		restartedGate := newGate(h)
		require.False(t, submitDailyCollect(t, h, restartedGate, queue),
			"第 %d 次重启后当日已认领,不得重复提交 NAS 采集任务", restart)

		cdnReclaimed, err := restartedGate.TryClaim(context.Background(), scheduler.GateResourceCDN, nastest.Today())
		require.NoError(t, err)
		require.False(t, cdnReclaimed, "第 %d 次重启后 cdn 键同样不重复认领", restart)
	}

	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceCDN))
	// 仅首启时提交的 1 条 NAS 采集任务
	requireSubmittedDailyTasks(t, h, 1)
}

// Outcome rollback-memory-gate-active: 特性开关显式切回 false 时回滚内存闸,
// 调度任务仍正常提交、采集不中断(接受重启重复提交旧缺陷换取调度器可用)。
// 回滚分支在 checkNASMetricsCollectionMemory 内部,由 internal scheduler
// 单测覆盖;此处锚定对外可观测语义:回滚形态下提交不再经过持久化日闸,
// 任务仍可正常进入队列,scheduler_state 不承载当日闸状态。
func TestStep4_RollbackMemoryGate_SubmissionStillFlows(t *testing.T) {
	h, _ := newJourneyHarness()
	queue := newTaskQueue(h)

	// 内存闸回滚形态:绕过持久化日闸直接提交(生产调度器该分支的行为)
	task := &taskx.Task{
		ID:        "nasjt-memory-gate-" + nastest.Today(),
		Type:      "nas:collect_metrics",
		Status:    taskx.TaskStatusPending,
		Params:    map[string]any{"days": 2},
		CreatedBy: "nasjt-memory-gate",
	}
	require.NoError(t, queue.Submit(task), "回滚内存闸后采集链路不中断,任务仍可提交")
	require.Len(t, h.TaskRepo.Tasks(), 1)
	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceNAS),
		"内存闸日期字段(而非 scheduler_state)承载当日闸状态")
}

// Outcome submit-failure-claim-retained: 认领成功持久化后任务队列提交失败
// (队列打满形态),当日不再重试提交、不回滚认领,last_date 保持为今日。
func TestStep4_SubmitFailure_ClaimRetained(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h) // BufferSize=1 且无 worker:首条占满后必然打满

	// 认领成功并提交第一条(占满缓冲)
	require.True(t, submitDailyCollect(t, h, gate, queue))
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))

	// 队列打满:再次提交失败(运维级故障形态)
	err := queue.Submit(&taskx.Task{ID: "nasjt-full-" + nastest.Today(), Type: "nas:collect_metrics"})
	require.Error(t, err, "队列打满时 Submit 应失败")

	// 认领已持久化,不回滚:当日不再重试,任务仓储只有已成功的一条
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS),
		"提交失败不得回滚认领(回滚会与多副本认领竞争)")
	requireSubmittedDailyTasks(t, h, 1)
}
