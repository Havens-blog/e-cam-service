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
	"fmt"
	"time"

	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/gotomicro/ego/core/elog"
)

// gateAlertEscalateThreshold 日闸连续故障达到该轮数后告警升为 critical
const gateAlertEscalateThreshold = 3

// AlertEventSink cam 域所需的告警事件落库端口(消费方接口):仅 CreateEvent,
// 绕过规则匹配直落。alert 仓储 AlertDAO 结构性满足;本桥依赖 alert domain
// 契约而非其 repository/dao(depcheck R1:跨域不得触达对方持久化层)。
type AlertEventSink interface {
	CreateEvent(ctx context.Context, event alertdomain.AlertEvent) (int64, error)
}

// schedulerGateAlerter 日闸告警桥:DailyGateAlerter 的生产实现
type schedulerGateAlerter struct {
	alertDAO AlertEventSink
}

// NewSchedulerGateAlerter 创建日闸告警桥(返回具体类型以同时满足
// scheduler.DailyGateAlerter 与 executor.NASHealthAlerter 两个通道接口,
// 同一实例装配两处——「同一定义勿重复造」)
func NewSchedulerGateAlerter(alertDAO AlertEventSink) *schedulerGateAlerter {
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

// AlertNASZeroSuccess NAS 自我健康监控升级告警:必达厂商连续 windowDays 天
// 零成功采集、且实盘存在 ≥1 个 NAS 实例(前置已由执行器核验)。与日闸故障
// 告警共用同一告警通道(同一 CreateEvent 直落库路径,勿重复造);因触发条件
// 本身已蕴含「连续 3 天静默失效」,直接落 critical(页面级),不走 warning 逐级。
func (a *schedulerGateAlerter) AlertNASZeroSuccess(ctx context.Context, provider string, windowDays int, instanceCount int64) {
	if a == nil || a.alertDAO == nil {
		return
	}
	evt := alertdomain.AlertEvent{
		Type: alertdomain.AlertTypeSyncFailure,
		// 绕过规则匹配直接落库:告警事件必须持久可查,投递由告警通知链路接管
		Status:   alertdomain.EventStatusPending,
		Severity: alertdomain.SeverityCritical,
		Title:    "NAS 指标采集连续零成功(自我健康监控)",
		Content: map[string]any{
			"summary": fmt.Sprintf(
				"必达厂商 %s 已连续 %d 天零成功采集,但实盘存在 %d 个 NAS 实例",
				provider, windowDays, instanceCount),
			"provider":      provider,
			"window_days":   windowDays,
			"nas_instances": instanceCount,
			"impact":        "该厂商 NAS 容量指标可能已静默失效,运营视图将出现数据空窗,请排查适配器/云账号凭证/厂商监控 API",
		},
		Source:     "scheduler:nas-health:" + provider,
		CreateTime: time.Now(),
	}
	if _, cerr := a.alertDAO.CreateEvent(ctx, evt); cerr != nil {
		elog.Error("NAS 零成功告警事件落库失败",
			elog.String("provider", provider),
			elog.FieldErr(cerr))
	}
}

// AlertOSSZeroSuccess OSS 自我健康监控升级告警:必达厂商连续 windowDays 天
// 零成功采集、且实盘存在 ≥1 个 OSS bucket(前置已由执行器核验)。与日闸故障/
// NAS 健康监控共用同一告警通道(同一 CreateEvent 直落库路径,勿重复造);
// 因触发条件本身已蕴含「连续 3 天静默失效」,直接落 critical(页面级),
// 不走 warning 逐级——语义与 AlertNASZeroSuccess 完全同型。
func (a *schedulerGateAlerter) AlertOSSZeroSuccess(ctx context.Context, provider string, windowDays int, bucketCount int64) {
	if a == nil || a.alertDAO == nil {
		return
	}
	evt := alertdomain.AlertEvent{
		Type: alertdomain.AlertTypeSyncFailure,
		// 绕过规则匹配直接落库:告警事件必须持久可查,投递由告警通知链路接管
		Status:   alertdomain.EventStatusPending,
		Severity: alertdomain.SeverityCritical,
		Title:    "OSS 指标采集连续零成功(自我健康监控)",
		Content: map[string]any{
			"summary": fmt.Sprintf(
				"必达厂商 %s 已连续 %d 天零成功采集,但实盘存在 %d 个 OSS bucket",
				provider, windowDays, bucketCount),
			"provider":    provider,
			"window_days": windowDays,
			"oss_buckets": bucketCount,
			"impact":      "该厂商 OSS 容量指标可能已静默失效,运营视图将出现数据空窗,请排查适配器/云账号凭证/厂商监控 API",
		},
		Source:     "scheduler:oss-health:" + provider,
		CreateTime: time.Now(),
	}
	if _, cerr := a.alertDAO.CreateEvent(ctx, evt); cerr != nil {
		elog.Error("OSS 零成功告警事件落库失败",
			elog.String("provider", provider),
			elog.FieldErr(cerr))
	}
}

// AlertDiskZeroSuccess Disk 自我健康监控升级告警:必达厂商连续 windowDays 天
// 零成功采集、且实盘存在 ≥1 个 Disk 实例(前置已由执行器核验)。与日闸故障/
// NAS/OSS 健康监控共用同一告警通道(同一 CreateEvent 直落库路径,勿重复造);
// 因触发条件本身已蕴含「连续 3 天静默失效」,直接落 critical(页面级),
// 不走 warning 逐级——语义与 AlertNASZeroSuccess/AlertOSSZeroSuccess 完全同型。
func (a *schedulerGateAlerter) AlertDiskZeroSuccess(ctx context.Context, provider string, windowDays int, instanceCount int64) {
	if a == nil || a.alertDAO == nil {
		return
	}
	evt := alertdomain.AlertEvent{
		Type: alertdomain.AlertTypeSyncFailure,
		// 绕过规则匹配直接落库:告警事件必须持久可查,投递由告警通知链路接管
		Status:   alertdomain.EventStatusPending,
		Severity: alertdomain.SeverityCritical,
		Title:    "Disk 指标采集连续零成功(自我健康监控)",
		Content: map[string]any{
			"summary": fmt.Sprintf(
				"必达厂商 %s 已连续 %d 天零成功采集,但实盘存在 %d 个 Disk 实例",
				provider, windowDays, instanceCount),
			"provider":       provider,
			"window_days":    windowDays,
			"disk_instances": instanceCount,
			"impact":         "该厂商 Disk 使用率/性能指标可能已静默失效,运营视图将出现数据空窗,请排查适配器/云账号凭证/厂商监控 API",
		},
		Source:     "scheduler:disk-health:" + provider,
		CreateTime: time.Now(),
	}
	if _, cerr := a.alertDAO.CreateEvent(ctx, evt); cerr != nil {
		elog.Error("Disk 零成功告警事件落库失败",
			elog.String("provider", provider),
			elog.FieldErr(cerr))
	}
}
