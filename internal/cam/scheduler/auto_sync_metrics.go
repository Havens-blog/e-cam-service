package scheduler

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// 每日 CDN 指标采集的调度触发器。
//
// 背景:cdn:collect_metrics 执行器已注册(module.go),但此前无定时触发,
// 指标表会一直为空,经营视图(流量/命中率/成本分摊)无数据可展示。
// 本触发器挂在 AutoSyncScheduler 的分钟级检查循环上:
// 每天 Asia/Shanghai 首次检查时提交一次全局采集任务(days=2,补采昨日+今日),
// 同日不重复;提交失败不更新日期,下轮重试。
//
// T8 迁移:默认(特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 开启)经持久化
// 日闸 cdn 键原子认领后提交(重启不重复提交,内存闸缺陷修复);开关显式
// 关闭时回滚到内存闸 lastMetricsCollectDate(调度可用性优先,接受重启重复
// 提交旧缺陷,spec「特性开关与回滚」)。
//
// 注意:指标采集不依赖账号的 EnableAutoSync——即使账号未开资产自动同步,
// CDN 指标也应采集(经营视图覆盖全部活跃账号)。故触发在 checkAndSync 的
// 账号循环之外。

// metricCollectDate 返回 Asia/Shanghai 当日日期(YYYY-MM-DD)。
// 调度时区与采集执行器(cdnMetricsDateRange)一致,避免服务器 UTC 导致
// 月初/日界偏移。
func metricCollectDate(now time.Time) string {
	return now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
}

// shouldTriggerDailyMetric 判断今日是否需触发每日指标采集:
// lastDate 为空(从未触发)或与今日不同(跨日)时返回 true。
func shouldTriggerDailyMetric(lastDate string, now time.Time) bool {
	return lastDate != metricCollectDate(now)
}

// checkMetricsCollection 每日触发一次 CDN 指标采集。
// 默认走持久化日闸(cdn 键,原子认领);特性开关显式关闭时回滚内存闸。
func (s *AutoSyncScheduler) checkMetricsCollection() {
	if s.persistentGateEnabled {
		if s.dailyGate == nil {
			// 未装配持久化日闸(如未接 mongo 的最小装配):安全跳过,不降级回
			// 内存闸——内存闸的重启重复提交缺陷正是本闸要修的问题,不做半吊子回退。
			return
		}
		s.checkMetricsCollectionPersistent()
		return
	}
	// 回滚模式:开关显式关闭,切回内存闸
	s.checkMetricsCollectionMemory()
}

// checkMetricsCollectionPersistent 持久化日闸版 CDN 每日采集:
// 经 findOneAndUpdate 原子认领 cdn 键当日,认领成功才提交(唯一提交入口)。
// 首部署 scheduler_state 无 cdn 记录 → 视为首次认领,认领后触发一次当日
// 提交(days=2 只覆盖昨日+今日),不回溯补采历史(spec 过渡行为定义)。
func (s *AutoSyncScheduler) checkMetricsCollectionPersistent() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	today := metricCollectDate(s.nowFn())
	claimed, err := s.dailyGate.TryClaim(ctx, GateResourceCDN, today)
	if err != nil {
		s.logger.Error("CDN 持久化日闸认领失败,下轮重试", elog.FieldErr(err))
		return
	}
	if !claimed {
		// 已认领/他人已认领/退避窗口内:静默跳过
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// days=2:凌晨补采时「今日」行只有约 1 小时数据,
			// 必须连昨日完整数据一起采集(见 sync_cdn_metrics.go 注释)
			"days": 2,
		},
		Progress:  0,
		Message:   "每日 CDN 指标采集任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		// 认领已持久化,提交失败不回滚认领(回滚会与多副本认领竞争,且队列
		// Submit 仅在队列关闭/打满时失败,属运维级故障):ERROR 日志留痕排查。
		s.logger.Error("提交 CDN 指标采集任务失败(日闸已认领当日,今日不再重试)",
			elog.String("task_id", taskID),
			elog.String("date", today),
			elog.FieldErr(err))
		return
	}

	s.logger.Info("每日 CDN 指标采集任务已提交(持久化日闸认领成功)",
		elog.String("task_id", taskID),
		elog.String("date", today))
}

// checkMetricsCollectionMemory 内存闸版 CDN 每日采集(仅特性开关回滚时使用)。
func (s *AutoSyncScheduler) checkMetricsCollectionMemory() {
	now := s.nowFn()

	s.mu.Lock()
	shouldTrigger := shouldTriggerDailyMetric(s.lastMetricsCollectDate, now)
	s.mu.Unlock()
	if !shouldTrigger {
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// days=2:凌晨补采时「今日」行只有约 1 小时数据,
			// 必须连昨日完整数据一起采集(见 sync_cdn_metrics.go 注释)
			"days": 2,
		},
		Progress:  0,
		Message:   "每日 CDN 指标采集任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		s.logger.Error("提交 CDN 指标采集任务失败,下轮重试",
			elog.FieldErr(err))
		return // 不更新 lastMetricsCollectDate
	}

	s.mu.Lock()
	s.lastMetricsCollectDate = metricCollectDate(now)
	s.mu.Unlock()

	s.logger.Info("每日 CDN 指标采集任务已提交(内存闸回滚模式)",
		elog.String("task_id", taskID),
		elog.String("date", s.lastMetricsCollectDate))
}
