package scheduler

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// NAS 每日指标采集的调度触发器(持久化日闸版)。
//
// 背景:cdn:collect_metrics 的内存闸(auto_sync_metrics.go lastMetricsCollectDate)
// 在服务重启后会重复提交采集任务(实测一次重启重复提交 25+ 条)。NAS 每日采集
// 自上线起即采用持久化日闸(daily_gate.go):
//   - 分钟级循环经 findOneAndUpdate 原子认领当日(条件 last_date<today),认领
//     成功才提交 nas:collect_metrics(days=2,补昨日完整行+今日初态)——
//     原子认领是唯一提交入口(Hard Rule);多副本/手动+自动重叠时同一资源只被
//     一个实例认领;
//   - 认领持久化在 scheduler_state(mongo),服务重启当日不重复提交;
//   - 首次无 scheduler_state 记录视为首次认领:认领后触发一次当日提交,
//     不回溯补采历史(spec 过渡行为定义);
//   - 写失败指数退避重试+升级告警、读失败 ≥5 分钟退避,降级语义见 daily_gate.go。
//
// CDN 已迁移到本闸(T8):CDN 默认走持久化日闸 cdn 键(auto_sync_metrics.go);
// 特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭时 NAS/CDN 一并回滚到
// 内存闸(本文件 checkNASMetricsCollectionMemory 回退路径)。
//
// 注意:与 CDN 一致,指标采集不依赖账号的 EnableAutoSync——触发在 checkAndSync
// 的账号循环之外。

// checkNASMetricsCollection 每日触发一次 NAS 指标采集。
// 默认走持久化日闸原子认领;特性开关显式关闭时回滚内存闸(采集不中断)。
func (s *AutoSyncScheduler) checkNASMetricsCollection() {
	if !s.persistentGateEnabled {
		// 特性开关回滚(Hard Rule:生产行为变更必须有退路):切回内存闸,
		// 接受重启重复提交旧缺陷换取调度器可用(spec「特性开关与回滚」)
		s.checkNASMetricsCollectionMemory()
		return
	}

	if s.dailyGate == nil {
		// 未装配持久化日闸(如未接 mongo 的最小装配):安全跳过,不降级回内存闸
		// ——内存闸的重启重复提交缺陷正是本闸要修的问题,不做半吊子回退。
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	today := metricCollectDate(s.nowFn())
	claimed, err := s.dailyGate.TryClaim(ctx, GateResourceNAS, today)
	if err != nil {
		s.logger.Error("NAS 持久化日闸认领失败,下轮重试", elog.FieldErr(err))
		return
	}
	if !claimed {
		// 已认领/他人已认领/退避窗口内:静默跳过
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeNASCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// days=2:凌晨补采时「今日」行只有约 1 小时数据,
			// 必须连昨日完整数据一起采集(与 CDN days=2 同语义)
			"days": 2,
		},
		Progress:  0,
		Message:   "每日 NAS 指标采集任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		// 认领已持久化,提交失败不回滚认领(回滚会与多副本认领竞争,且队列
		// Submit 仅在队列关闭/打满时失败,属运维级故障):ERROR 日志留痕排查。
		s.logger.Error("提交 NAS 指标采集任务失败(日闸已认领当日,今日不再重试)",
			elog.String("task_id", taskID),
			elog.String("date", today),
			elog.FieldErr(err))
		return
	}

	s.logger.Info("每日 NAS 指标采集任务已提交(持久化日闸认领成功)",
		elog.String("task_id", taskID),
		elog.String("date", today))
}

// checkNASMetricsCollectionMemory 内存闸版 NAS 每日采集
// (仅特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭的回滚模式使用)。
func (s *AutoSyncScheduler) checkNASMetricsCollectionMemory() {
	now := s.nowFn()

	s.mu.Lock()
	shouldTrigger := shouldTriggerDailyMetric(s.lastNASMetricsCollectDate, now)
	s.mu.Unlock()
	if !shouldTrigger {
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeNASCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// days=2:凌晨补采时「今日」行只有约 1 小时数据,
			// 必须连昨日完整数据一起采集(与 CDN days=2 同语义)
			"days": 2,
		},
		Progress:  0,
		Message:   "每日 NAS 指标采集任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		s.logger.Error("提交 NAS 指标采集任务失败,下轮重试",
			elog.FieldErr(err))
		return // 不更新内存闸日期,下轮重试
	}

	s.mu.Lock()
	s.lastNASMetricsCollectDate = metricCollectDate(now)
	s.mu.Unlock()

	s.logger.Info("每日 NAS 指标采集任务已提交(内存闸回滚模式)",
		elog.String("task_id", taskID),
		elog.String("date", s.lastNASMetricsCollectDate))
}
