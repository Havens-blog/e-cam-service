package scheduler

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
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

// checkMetricsCollection 每日触发一次 CDN 指标采集(幂等,同日不重复)。
func (s *AutoSyncScheduler) checkMetricsCollection() {
	now := time.Now()

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

	s.logger.Info("每日 CDN 指标采集任务已提交",
		elog.String("task_id", taskID),
		elog.String("date", s.lastMetricsCollectDate))
}
