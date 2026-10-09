package scheduler

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// GateResourceRDS RDS 日闸资源类型分键。声明在本文件自身触发文件内,与
// NAS 回填在 auto_sync_nas_backfill.go 声明 GateResourceNASBackfill 同一先例;
// scheduler_state 各资源(nas/cdn/oss/disk/rds)独立分键,互不覆盖(Hard Rule)。
const GateResourceRDS = "rds"

// RDS 每日指标采集的调度触发器(持久化日闸版)。
//
// 平移蓝本:auto_sync_disk_metrics.go(第五次资源接入,零新增调度机制):
// disk/nas/oss 的日闸与回滚结构原样复用,仅替换资源键 rds 与任务类型
// rds:collect_metrics。背景:rds:collect_metrics 执行器已注册
// (sync_rds_metrics.go),本触发器把它挂上 AutoSyncScheduler 的分钟级检查循环,
// 复用 scheduler_state 持久化日闸(proposal「持久化日闸复用」):
//   - 分钟级循环经 findOneAndUpdate 原子认领 rds 键当日(条件
//     resource_type=rds AND last_date<today),认领成功才提交
//     rds:collect_metrics(days=2,补昨日完整行+今日初态)——原子认领是唯一
//     提交入口(Hard Rule);多副本/手动+自动重叠时同一资源只被一个实例认领;
//   - 认领持久化在 scheduler_state(mongo),服务重启当日不重复提交;
//   - 首次无 scheduler_state rds 记录视为首次认领:认领后触发一次当日提交,
//     不回溯补采历史(spec 过渡行为定义);
//   - 写失败指数退避重试+升级告警、读失败 ≥5 分钟退避,降级语义见 daily_gate.go;
//   - 特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭时回滚到内存闸
//     (采集不中断;NAS/CDN/OSS/Disk 一并回滚,见 auto_sync_nas_metrics.go /
//     auto_sync_disk_metrics.go)。
//
// 注意:与 NAS/OSS/Disk 一致,指标采集不依赖账号的 EnableAutoSync——触发在
// checkAndSync 的账号循环之外。

// checkRDSMetricsCollection 每日触发一次 RDS 指标采集。
// 默认走持久化日闸 rds 键原子认领;特性开关显式关闭时回滚内存闸(采集不中断)。
func (s *AutoSyncScheduler) checkRDSMetricsCollection() {
	if !s.persistentGateEnabled {
		// 特性开关回滚(Hard Rule:生产行为变更必须有退路):切回内存闸,
		// 接受重启重复提交旧缺陷换取调度器可用(spec「特性开关与回滚」)
		s.checkRDSMetricsCollectionMemory()
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
	claimed, err := s.dailyGate.TryClaim(ctx, GateResourceRDS, today)
	if err != nil {
		s.logger.Error("RDS 持久化日闸认领失败,下轮重试", elog.FieldErr(err))
		return
	}
	if !claimed {
		// 已认领/他人已认领/退避窗口内:静默跳过
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeRDSCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// days=2:凌晨补采时「今日」行只有约 1 小时数据,
			// 必须连昨日完整数据一起采集(与 NAS/OSS/Disk days=2 同语义)
			"days": 2,
		},
		Progress:  0,
		Message:   "每日 RDS 指标采集任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		// 认领已持久化,提交失败不回滚认领(回滚会与多副本认领竞争,且队列
		// Submit 仅在队列关闭/打满时失败,属运维级故障):ERROR 日志留痕排查。
		s.logger.Error("提交 RDS 指标采集任务失败(日闸已认领当日,今日不再重试)",
			elog.String("task_id", taskID),
			elog.String("date", today),
			elog.FieldErr(err))
		return
	}

	s.logger.Info("每日 RDS 指标采集任务已提交(持久化日闸认领成功)",
		elog.String("task_id", taskID),
		elog.String("date", today))
}

// checkRDSMetricsCollectionMemory 内存闸版 RDS 每日采集
// (仅特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭的回滚模式使用)。
func (s *AutoSyncScheduler) checkRDSMetricsCollectionMemory() {
	now := s.nowFn()

	s.mu.Lock()
	shouldTrigger := shouldTriggerDailyMetric(s.lastRDSMetricsCollectDate, now)
	s.mu.Unlock()
	if !shouldTrigger {
		return
	}

	taskID := uuid.New().String()
	task := &taskx.Task{
		ID:     taskID,
		Type:   executor.TaskTypeRDSCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{
			// days=2:凌晨补采时「今日」行只有约 1 小时数据,
			// 必须连昨日完整数据一起采集(与 NAS/OSS/Disk days=2 同语义)
			"days": 2,
		},
		Progress:  0,
		Message:   "每日 RDS 指标采集任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		s.logger.Error("提交 RDS 指标采集任务失败,下轮重试",
			elog.FieldErr(err))
		return // 不更新内存闸日期,下轮重试
	}

	s.mu.Lock()
	s.lastRDSMetricsCollectDate = metricCollectDate(now)
	s.mu.Unlock()

	s.logger.Info("每日 RDS 指标采集任务已提交(内存闸回滚模式)",
		elog.String("task_id", taskID),
		elog.String("date", s.lastRDSMetricsCollectDate))
}
