// Package cam 持久化日闸故障告警桥。
//
// 文件：internal/cam/scheduler_gate_alerter.go
//
// 作用：把 scheduler.PersistentDailyGate 的升级告警落到告警模块——直接经
// alert DAO CreateEvent 落库(与 cert_alert_publisher 同模式,绕过规则匹配,
// 保证故障一定留下告警事件记录;pending 事件由告警通知链路投递渠道)。
// Hard Rule:日闸写失败必须重试+告警,不得仅记日志。
//
// T6 自我健康监控、T8 CDN 迁移共用本告警通道(勿重复造)。
package cam

import (
	"context"
	"time"

	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	alertdao "github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/gotomicro/ego/core/elog"
)

// gateAlertEscalateThreshold 日闸连续故障达到该轮数后告警升为 critical
const gateAlertEscalateThreshold = 3

// schedulerGateAlerter 日闸告警桥:DailyGateAlerter 的生产实现
type schedulerGateAlerter struct {
	alertDAO alertdao.AlertDAO
}

// NewSchedulerGateAlerter 创建日闸告警桥(装配进 PersistentDailyGate)
func NewSchedulerGateAlerter(alertDAO alertdao.AlertDAO) scheduler.DailyGateAlerter {
	return &schedulerGateAlerter{alertDAO: alertDAO}
}

// AlertDailyGateFailure 上报日闸故障:落一条告警事件(绕过规则匹配)。
// 连续失败轮数 < gateAlertEscalateThreshold 为 warning,达到后升为 critical
// (升级语义,避免首轮瞬时抖动即最高级别)。
func (a *schedulerGateAlerter) AlertDailyGateFailure(ctx context.Context, resourceType, operation string, consecutiveFailures int, err error) {
	if a == nil || a.alertDAO == nil {
		return
	}
	severity := alertdomain.SeverityWarning
	if consecutiveFailures >= gateAlertEscalateThreshold {
		severity = alertdomain.SeverityCritical
	}
	evt := alertdomain.AlertEvent{
		Type: alertdomain.AlertTypeSyncFailure,
		// 绕过规则匹配直接落库:告警事件必须持久可查(非仅日志),投递由
		// ProcessPendingEvents 渠道链路接管。
		Status:   alertdomain.EventStatusPending,
		Severity: severity,
		Title:    "持久化日闸故障(调度器 scheduler_state)",
		Content: map[string]any{
			"summary":       "持久化日闸故障,采集任务触发降级中",
			"resource_type": resourceType,
			"operation":     operation,
			"failures":      consecutiveFailures,
			"error":         err.Error(),
			"impact":        "该资源每日指标采集在故障期间暂停,恢复后自动续采(不回溯补采)",
		},
		Source:     "scheduler:gate:" + resourceType,
		CreateTime: time.Now(),
	}
	if _, cerr := a.alertDAO.CreateEvent(ctx, evt); cerr != nil {
		// 告警落库失败只能退回日志(此时 mongo 已故障,属预期);调度器侧
		// 退避窗口仍生效,不会洪泛。
		elog.Error("日闸告警事件落库失败",
			elog.String("resource_type", resourceType),
			elog.String("operation", operation),
			elog.FieldErr(cerr))
	}
}
