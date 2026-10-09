package scheduler

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// NAS 历史指标回填的调度触发器(一次性上线回填,错峰窗口内自动提交)。
//
// spec「回填配额节流(与每日采集碰撞规避)」:回填在 01:30~06:00(Asia/Shanghai)
// 窗口执行,与每日自动采集(00:10 后)不碰撞;命中限流的厂商回填挂起、次日
// 窗口续跑。续跑机制 = 本触发器每窗口提交一次 + 执行器唯一键幂等去重:
//   - 分钟级循环在窗口内经持久化日闸(resource=nas_backfill)原子认领当日,
//     认领成功才提交 nas:backfill_metrics(默认 30 天)——每日一次,多副本不重复;
//   - 执行器窗口外触发直接跳过、已成功批次(区间全已落库)零调用跳过、只对
//     缺失日期回源补采——因此限流/窗口到期的挂起天然在次日窗口从缺失处继续;
//   - 回填完成后的后续窗口提交为去重空跑(仅本地枚举 + 预检查询,不调厂商
//     API),同时覆盖「新纳管账号补历史」场景,无需单独开关。
//
// 与每日采集一致:回填不依赖账号的 EnableAutoSync 开关,触发在 checkAndSync
// 的账号循环之外。

// GateResourceNASBackfill 回填日闸资源类型(scheduler_state)
const GateResourceNASBackfill = "nas_backfill"

// checkNASMetricsBackfill 回填窗口内每日提交一次 NAS 历史回填任务
// (持久化日闸原子认领,窗口外静默跳过)。
func (s *AutoSyncScheduler) checkNASMetricsBackfill() {
	if s.dailyGate == nil {
		// 未装配持久化日闸(如未接 mongo 的最小装配):安全跳过
		return
	}
	// 错峰窗口外不提交(执行器侧亦有同判定兜底,双保险不消耗厂商配额)
	now := s.nowFn()
	if !executor.NASBackfillWindowActive(now) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	today := metricCollectDate(now)
	claimed, err := s.dailyGate.TryClaim(ctx, GateResourceNASBackfill, today)
	if err != nil {
		s.logger.Error("NAS 回填日闸认领失败,下轮重试", elog.FieldErr(err))
		return
	}
	if !claimed {
		// 本窗口已提交过/他人已认领:静默跳过
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeNASBackfillMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// 默认回填 30 天(14~90 天区间,由执行器钳位)
			"days": 30,
		},
		Progress:  0,
		Message:   "NAS 历史指标回填任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		// 认领已持久化,提交失败不回滚认领(与每日采集同语义,运维级故障):
		// ERROR 日志留痕,次日窗口仍会续跑(幂等去重保证不产生脏行)。
		s.logger.Error("提交 NAS 历史回填任务失败(日闸已认领当日)",
			elog.String("task_id", taskID),
			elog.String("date", today),
			elog.FieldErr(err))
		return
	}

	s.logger.Info("NAS 历史指标回填任务已提交(窗口内日闸认领成功)",
		elog.String("task_id", taskID),
		elog.String("date", today))
}
