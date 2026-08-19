// Package scheduler cert 域调度接线（任务 7.1）。
//
// 本文件是 9 类定时任务的注册清单（tech-design Scheduler Tasks 表）：
// 仅做调度接线——频率、防重守卫与结果通知，不内联业务逻辑（Hard Rule：
// 业务逻辑在 3.5/4.4/5.9/5.10 已实现的恢复/Job 函数）。
//
// 9 类任务 → 8 个调度点的映射（实现取舍，与 4.4/5.10 已实现形态一致）：
//
//	cert:scan             scan            天级 02:00（手动触发走 POST /:id/scan，防重 409）
//	cert:scan-timeout     scan-timeout    每 15 分钟（超时快照转 failed 释放防重锁）
//	cert:inspection       inspection      天级 05:00（4.4 InspectionJob 流水线，
//	                                       第三步即 probe 天级调度点——表内 probe
//	                                       行的天级节奏由此承载，不重复注册独立
//	                                       probe 任务避免同日双重全网拨测）
//	cert:verify-window    probe 提频 +    周期 = verifyProbeIntervalMinutes（5.10
//	                       window-expiry   扫描式承载：ProbeVerifyingWindows 提频
//	                                       探测 + FinalizeExpiredWindows 终局判定，
//	                                       每个终局订单即时消费孤儿清理队列 =
//	                                       orphan-cleanup 的事件触发路径）
//	cert:pause-timeout    pause-timeout   每小时（超时取消清单逐单发 ops 处置通知）
//	cert:orphan-sweep     orphan-cleanup  天级 03:00 批扫（ListByStatus 兜底）
//	cert:crd-recheck      crd-recheck     分钟级扫描（5.9 扫描式延迟消费：到期 =
//	                                       executedAt+recheckDelayMinutes（默认
//	                                       5 分钟），分钟级扫描保证延迟契约）
//	cert:executing-timeout executing-timeout 每 5 分钟（心跳超时项恢复）
//
// 调度挂载：ioc/cert.go InitCertJobs 按本清单构建 ecron 组件（cronjob.enabled
// 同门控），装配风格对齐既有 ioc/jobs.go（cam 域任务挂载机制）。
//
// 频率/阈值参数（AC-6）：verifyProbeIntervalMinutes 经 ResolveVerifyProbeIntervalMinutes
// 从 AlertConfig.thresholds 装配期读取（DB 单文档，运行期改动需重启生效）；
// scanTimeoutHours/pauseTimeoutHours/itemHeartbeatTimeoutMinutes/recheckDelayMinutes
// 由各服务函数运行期自行读取（3.5/5.1/5.7/5.9 已实现）。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// ---------------------------------------------------------------------
// 调度点名称与频率（Scheduler Tasks 表对表登记）
// ---------------------------------------------------------------------

// 调度点名称（日志/测试断言锚点；与表行对应关系见包注释）。
const (
	JobScan             = "cert:scan"              // scan：五云引用发现
	JobScanTimeout      = "cert:scan-timeout"      // scan-timeout：超时快照恢复
	JobInspection       = "cert:inspection"        // inspection + probe 天级
	JobVerifyWindow     = "cert:verify-window"     // probe 提频 + window-expiry
	JobPauseTimeout     = "cert:pause-timeout"     // pause-timeout：批间暂停超时取消
	JobOrphanSweep      = "cert:orphan-sweep"      // orphan-cleanup：天级批扫
	JobCrdRecheck       = "cert:crd-recheck"       // crd-recheck：到期复检扫描
	JobExecutingTimeout = "cert:executing-timeout" // executing-timeout：心跳超时恢复
)

// 固定频率调度点的 cron 表达式（5 字段：分 时 日 月 周，ego ecron 默认解析器）。
const (
	SpecScanDaily          = "0 2 * * *"    // scan 天级（02:00 低峰）
	SpecScanTimeoutQuarter = "*/15 * * * *" // scan-timeout 每 15 分钟（表值）
	SpecInspectionDaily    = "0 5 * * *"    // inspection 天级（05:00，与 scan 错峰）
	SpecPauseTimeoutHourly = "0 * * * *"    // pause-timeout 每小时（表值）
	SpecOrphanSweepDaily   = "0 3 * * *"    // orphan-cleanup 天级批扫（03:00）
	SpecCrdRecheckMinutely = "* * * * *"    // crd-recheck 分钟级（延迟契约承载）
	SpecExecutingTimeout5m = "*/5 * * * *"  // executing-timeout 每 5 分钟（表值）
)

// ---------------------------------------------------------------------
// 窄端口（装配即编译期对齐既有服务，测试零成本替身）
// ---------------------------------------------------------------------

// ScanRunner scan + scan-timeout 入口（3.5 ReferenceScanService）。
type ScanRunner interface {
	StartScan(ctx context.Context) (service.ScanResult, error)
	RecoverTimedOutScans(ctx context.Context) (int, error)
}

// InspectionRunner inspection 入口（4.4 InspectionJob，含 probe 天级步骤）。
type InspectionRunner interface {
	RunInspection(ctx context.Context) (InspectionRun, error)
}

// WindowRunner 提频探测 + 窗口终局入口（5.10 VerifyWindowService）。
type WindowRunner interface {
	ProbeVerifyingWindows(ctx context.Context) (int, error)
	FinalizeExpiredWindows(ctx context.Context) ([]string, error)
}

// PauseCanceller pause-timeout 入口（5.1 ChangeService.CancelByTimeout）。
type PauseCanceller interface {
	CancelByTimeout(ctx context.Context) ([]string, error)
}

// ExecutingRecoverer executing-timeout 入口（5.7 RecoverTimedOutItems）。
type ExecutingRecoverer interface {
	RecoverTimedOutItems(ctx context.Context) (int, error)
}

// OrphanConsumer orphan-cleanup 双入口（5.9：事件消费 + 天级批扫）。
type OrphanConsumer interface {
	ConsumeOrderQueue(ctx context.Context, orderID string) (int, error)
	SweepOrphans(ctx context.Context) (int, error)
}

// RecheckConsumer crd-recheck 入口（5.9 RunDueRechecks）。
type RecheckConsumer interface {
	RunDueRechecks(ctx context.Context) (int, error)
}

// 编译期断言：既有服务实现满足调度窄端口。
var (
	_ ScanRunner         = (service.ReferenceScanService)(nil)
	_ InspectionRunner   = (*InspectionJob)(nil)
	_ WindowRunner       = (service.VerifyWindowService)(nil)
	_ PauseCanceller     = (service.ChangeService)(nil)
	_ ExecutingRecoverer = (service.ChangeExecuteService)(nil)
	_ OrphanConsumer     = (service.OrphanCleanupService)(nil)
	_ RecheckConsumer    = (service.CrdRecheckService)(nil)
)

// CertJobs 9 类定时任务的调度依赖集（ioc 装配注入，字段均为已实现服务）。
type CertJobs struct {
	Scan       ScanRunner                 // 3.5 引用扫描（scan + scan-timeout）
	Inspection InspectionRunner           // 4.4 巡检流水线（inspection + probe 天级）
	Windows    WindowRunner               // 5.10 验证窗口（提频 + 终局）
	Changes    PauseCanceller             // 5.1 批间暂停超时取消
	Execute    ExecutingRecoverer         // 5.7 心跳超时恢复
	Orphan     OrphanConsumer             // 5.9 孤儿清理
	Recheck    RecheckConsumer            // 5.9 CRD 复检
	Publisher  service.CertAlertPublisher // pause-timeout 处置通知（ops 类）
}

// CertJobSpec 单个调度点：名称 + cron 表达式 + 入口函数（已含守卫包装）。
type CertJobSpec struct {
	Name string                          // 调度点名称（Job* 常量）
	Spec string                          // cron 表达式（5 字段）
	Run  func(ctx context.Context) error // 入口（单飞守卫 + panic 恢复）
}

// JobSpecs 构建 8 个调度点（承载 9 类任务，映射见包注释）。
// verifyProbeIntervalMinutes 为窗口周期（装配期经 ResolveVerifyProbeIntervalMinutes
// 从 AlertConfig.thresholds 读取；非法值在 VerifyWindowSpec 内回退默认）。
func (j *CertJobs) JobSpecs(verifyProbeIntervalMinutes int) []CertJobSpec {
	return []CertJobSpec{
		{
			Name: JobScan,
			Spec: SpecScanDaily,
			Run: j.guarded(JobScan, func(ctx context.Context) error {
				// 防重联动：running 快照存在 → ErrScanInProgress 属预期跳过
				// （手动触发侧映射 409；调度侧静默让位）；scan-timeout 恢复
				// 释放锁后下一轮可正常触发。
				if _, err := j.Scan.StartScan(ctx); err != nil && !errors.Is(err, domain.ErrScanInProgress) {
					return fmt.Errorf("cert scan: %w", err)
				}
				return nil
			}),
		},
		{
			Name: JobScanTimeout,
			Spec: SpecScanTimeoutQuarter,
			Run: j.guarded(JobScanTimeout, func(ctx context.Context) error {
				if _, err := j.Scan.RecoverTimedOutScans(ctx); err != nil {
					return fmt.Errorf("cert scan-timeout: %w", err)
				}
				return nil
			}),
		},
		{
			Name: JobInspection,
			Spec: SpecInspectionDaily,
			Run: j.guarded(JobInspection, func(ctx context.Context) error {
				// 4.4 流水线：完整性复检 → 到期分级 → probe 天级 → 豁免过滤。
				// Job 自带单飞守卫（ErrInspectionInProgress），此处守卫双保险。
				if _, err := j.Inspection.RunInspection(ctx); err != nil && !errors.Is(err, ErrInspectionInProgress) {
					return fmt.Errorf("cert inspection: %w", err)
				}
				return nil
			}),
		},
		{
			Name: JobVerifyWindow,
			Spec: VerifyWindowSpec(verifyProbeIntervalMinutes),
			Run: j.guarded(JobVerifyWindow, func(ctx context.Context) error {
				// 提频探测（5.10 扫描式承载，实现取舍与 5.10 一致：统一扫描
				// 活跃窗口而非按订单登记提频定时器）。
				_, probeErr := j.Windows.ProbeVerifyingWindows(ctx)
				// 终局判定不依赖被动探测触发（Hard Rule）——探测失败不阻断。
				finalized, finErr := j.Windows.FinalizeExpiredWindows(ctx)
				// orphan-cleanup 事件触发：终局订单即时消费该单清理队列。
				var consumeErrs []error
				for _, orderID := range finalized {
					if _, err := j.Orphan.ConsumeOrderQueue(ctx, orderID); err != nil {
						consumeErrs = append(consumeErrs, fmt.Errorf("cert orphan consume %s: %w", orderID, err))
					}
				}
				return errors.Join(probeErr, finErr, errors.Join(consumeErrs...))
			}),
		},
		{
			Name: JobPauseTimeout,
			Spec: SpecPauseTimeoutHourly,
			Run: j.guarded(JobPauseTimeout, func(ctx context.Context) error {
				cancelled, err := j.Changes.CancelByTimeout(ctx)
				if err != nil {
					return fmt.Errorf("cert pause-timeout: %w", err)
				}
				// 超时取消处置通知（运维处置类，不计 PRD 四类业务告警口径）。
				var pubErrs []error
				for _, orderID := range cancelled {
					if pubErr := j.Publisher.PublishAlert(ctx, service.CertAlertEvent{
						Category: service.AlertCategoryOps,
						Title:    "变更单批间暂停超时自动取消",
						OrderID:  orderID,
						Detail:   fmt.Sprintf("change order %s cancelled by pause-timeout (unexecuted items skipped)", orderID),
						At:       time.Now(),
					}); pubErr != nil {
						pubErrs = append(pubErrs, fmt.Errorf("cert pause-timeout notify %s: %w", orderID, pubErr))
					}
				}
				return errors.Join(pubErrs...)
			}),
		},
		{
			Name: JobOrphanSweep,
			Spec: SpecOrphanSweepDaily,
			Run: j.guarded(JobOrphanSweep, func(ctx context.Context) error {
				if _, err := j.Orphan.SweepOrphans(ctx); err != nil {
					return fmt.Errorf("cert orphan-sweep: %w", err)
				}
				return nil
			}),
		},
		{
			Name: JobCrdRecheck,
			Spec: SpecCrdRecheckMinutely,
			Run: j.guarded(JobCrdRecheck, func(ctx context.Context) error {
				if _, err := j.Recheck.RunDueRechecks(ctx); err != nil {
					return fmt.Errorf("cert crd-recheck: %w", err)
				}
				return nil
			}),
		},
		{
			Name: JobExecutingTimeout,
			Spec: SpecExecutingTimeout5m,
			Run: j.guarded(JobExecutingTimeout, func(ctx context.Context) error {
				if _, err := j.Execute.RecoverTimedOutItems(ctx); err != nil {
					return fmt.Errorf("cert executing-timeout: %w", err)
				}
				return nil
			}),
		},
	}
}

// ---------------------------------------------------------------------
// 周期参数解析（AC-6：AlertConfig.thresholds → 调度周期）
// ---------------------------------------------------------------------

// ResolveVerifyProbeIntervalMinutes 从 AlertConfig.thresholds 读取窗口任务
// 周期（分钟）。读取失败/未配置/非法值回退 DefaultThresholds（10 分钟）——
// 窗口活性保障优先于配置精确性（周期拉长只延迟终局判定，不阻塞）。
func ResolveVerifyProbeIntervalMinutes(ctx context.Context, alertCfg domain.AlertConfigRepository) int {
	minutes := domain.DefaultThresholds().VerifyProbeIntervalMinutes
	if alertCfg == nil {
		return minutes
	}
	if cfg, err := alertCfg.Get(ctx); err == nil && cfg.Thresholds.VerifyProbeIntervalMinutes > 0 {
		minutes = cfg.Thresholds.VerifyProbeIntervalMinutes
	}
	return minutes
}

// VerifyWindowSpec 窗口周期分钟数 → cron 分钟域表达式。
// 整除 60 时取 */n；n>=60 取整点；非整除（7/9/45…）取 minute%n==0 集合，
// 小时边界处间隔不均（cron 表达能力限制，平均周期≈n，界值域 5~60）。
func VerifyWindowSpec(minutes int) string {
	if minutes <= 0 {
		minutes = domain.DefaultThresholds().VerifyProbeIntervalMinutes
	}
	if minutes >= 60 {
		return "0 * * * *"
	}
	return fmt.Sprintf("*/%d * * * *", minutes)
}

// ---------------------------------------------------------------------
// 守卫包装：单飞互斥 + panic 恢复（AC-3 框架级保障）
// ---------------------------------------------------------------------

// guarded 为任务入口包装两类框架级保障：
//  1. 单飞互斥：同一任务上一轮仍在执行时，本轮触发立即跳过（返回 nil），
//     不排队堆积——ecron 每次触发独立起 goroutine，无此守卫会重叠执行；
//  2. panic 恢复：入口 panic 转为 error 返回，不击穿调度框架进程。
func (j *CertJobs) guarded(name string, fn func(ctx context.Context) error) func(ctx context.Context) error {
	var (
		mu   sync.Mutex
		busy bool
	)
	return func(ctx context.Context) (err error) {
		mu.Lock()
		if busy {
			mu.Unlock()
			slog.Warn("cert job skipped: previous round still running", slog.String("job", name))
			return nil
		}
		busy = true
		mu.Unlock()

		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("cert job %s panicked: %v", name, r)
			}
			mu.Lock()
			busy = false
			mu.Unlock()
		}()
		return fn(ctx)
	}
}
