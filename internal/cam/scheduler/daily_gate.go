// Package scheduler 持久化日闸(原子认领 + 失败降级)。
//
// 文件：internal/cam/scheduler/daily_gate.go
//
// 作用：替代内存闸 lastMetricsCollectDate 的「每日一次」触发闸门
// (spec「持久化日闸 + 原子认领」,修「写失败/重启 → 重复提交采集任务」缺陷)。
//
// 三层兜底(proposal Key Risks):
//  1. 原子认领:一次 findOneAndUpdate(last_date < today)——多副本/手动+自动
//     重叠时同一资源只被一个实例认领(Hard Rule:认领成功才提交任务);
//  2. 写失败降级:指数退避重试,重试耗尽升级告警(Hard Rule:不得仅记日志,
//     否则「写失败+重启」让重启重复提交缺陷回归),并进入跨轮退避窗口;
//  3. 读失败退避:≥5 分钟退避窗口再重读,防挂在分钟级调度循环上逐分钟
//     洪泛 mongo 与任务队列。
//
// 资源类型分键(Hard Rule):nas/cdn 独立,互不覆盖。NAS 键 T7 接入;
// CDN 键 T8 迁移接入,特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED(默认开启)
// 提供回滚内存闸的退路(feature_flag.go)。
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

// 日闸资源类型分键(nas/cdn 独立,互不覆盖——Hard Rule)
const (
	GateResourceNAS = "nas"
	GateResourceCDN = "cdn"
)

// 日闸调参默认值
const (
	// gateReadBackoffWindow 读失败退避窗口(规格:≥5 分钟)
	gateReadBackoffWindow = 5 * time.Minute
	// gateMaxWriteRetries 单轮写失败重试次数(不含首次尝试)
	gateMaxWriteRetries = 3
	// gateWriteRetryDelay 单轮重试基础间隔,按 2^n 指数递增(1s, 2s, 4s, ...)
	gateWriteRetryDelay = 1 * time.Second
	// gateWriteBackoffBase 重试耗尽后的跨轮退避基础间隔,按 2^(连续失败-1)
	// 指数递增,上限 gateWriteBackoffMax(与读退避同量级,避免分钟级循环洪泛)
	gateWriteBackoffBase = 1 * time.Minute
	gateWriteBackoffMax  = 5 * time.Minute
)

// DailyGateStore 持久化日闸存储(由 dao.SchedulerStateDAO 实现,单测注入桩)
type DailyGateStore interface {
	TryClaimDaily(ctx context.Context, resourceType, date string) (bool, error)
	GetLastDate(ctx context.Context, resourceType string) (string, error)
}

// DailyGateAlerter 日闸故障升级告警通道(Hard Rule:写失败必须告警,不得仅记
// 日志)。T6 自我健康监控、T8 CDN 迁移共用同一告警通道(勿重复造)。
type DailyGateAlerter interface {
	// AlertDailyGateFailure 上报日闸故障:operation 为 "write"/"read",
	// consecutiveFailures 为连续故障轮数(供升级判断)。
	AlertDailyGateFailure(ctx context.Context, resourceType, operation string, consecutiveFailures int, err error)
}

// PersistentDailyGate 持久化日闸
//
// 调用次序(分钟级循环):读 last_date 判断是否需认领(读失败进入退避)→
// findOneAndUpdate 原子认领(写失败指数退避重试 + 升级告警)。认领成功才允许
// 提交采集任务——原子认领是唯一提交入口(Hard Rule)。
type PersistentDailyGate struct {
	store   DailyGateStore
	alerter DailyGateAlerter // 可为 nil(nil 时仅 ERROR 日志;生产装配告警实现)
	logger  *elog.Component
	// now 时钟注入(单测控制退避窗口推进)
	now func() time.Time

	// 调参(构造器给默认值,单测可收窄)
	readBackoffWindow time.Duration
	maxWriteRetries   int
	writeRetryDelay   time.Duration
	writeBackoffBase  time.Duration
	writeBackoffMax   time.Duration

	mu                sync.Mutex
	readBackoffUntil  time.Time // 读失败退避窗口截止
	writeBackoffUntil time.Time // 写失败跨轮退避窗口截止
	writeFailures     int       // 连续写失败轮数(指数退避与告警升级计数)
}

// NewPersistentDailyGate 创建持久化日闸
func NewPersistentDailyGate(store DailyGateStore, alerter DailyGateAlerter, logger *elog.Component) *PersistentDailyGate {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &PersistentDailyGate{
		store:             store,
		alerter:           alerter,
		logger:            logger,
		now:               time.Now,
		readBackoffWindow: gateReadBackoffWindow,
		maxWriteRetries:   gateMaxWriteRetries,
		writeRetryDelay:   gateWriteRetryDelay,
		writeBackoffBase:  gateWriteBackoffBase,
		writeBackoffMax:   gateWriteBackoffMax,
	}
}

// TryClaim 原子认领 resourceType 当日(today,Asia/Shanghai YYYY-MM-DD)触发权。
// 返回 (true, nil) 表示认领成功、调用方才可提交采集任务;(false, nil) 表示
// 已认领/他人已认领/处于退避窗口(静默跳过);写失败重试耗尽返回错误并告警。
func (g *PersistentDailyGate) TryClaim(ctx context.Context, resourceType, today string) (bool, error) {
	if g == nil || g.store == nil {
		return false, nil
	}

	// 退避窗口内静默跳过(读/写失败降级,防分钟级循环洪泛)
	now := g.now()
	g.mu.Lock()
	readBackedOff := now.Before(g.readBackoffUntil)
	writeBackedOff := now.Before(g.writeBackoffUntil)
	g.mu.Unlock()
	if readBackedOff || writeBackedOff {
		return false, nil
	}

	// 读:当日已认领则直接跳过(读失败 → ≥5 分钟退避窗口)
	lastDate, err := g.store.GetLastDate(ctx, resourceType)
	if err != nil {
		g.mu.Lock()
		g.readBackoffUntil = g.now().Add(g.readBackoffWindow)
		g.mu.Unlock()
		g.logger.Error("持久化日闸读取失败,进入退避窗口",
			elog.String("resource_type", resourceType),
			elog.Duration("backoff_window", g.readBackoffWindow),
			elog.FieldErr(err))
		return false, err
	}
	if lastDate == today {
		return false, nil
	}

	// 写:一次 findOneAndUpdate 原子认领;失败指数退避重试
	var lastErr error
	for attempt := 0; attempt <= g.maxWriteRetries; attempt++ {
		if attempt > 0 {
			// 指数退避:1s, 2s, 4s...(受 ctx 取消/超时约束,调度 ctx 30s)
			delay := g.writeRetryDelay << (attempt - 1)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				lastErr = ctx.Err()
				return false, lastErr
			case <-timer.C:
			}
		}
		claimed, err := g.store.TryClaimDaily(ctx, resourceType, today)
		if err == nil {
			g.mu.Lock()
			g.writeFailures = 0 // 任意成功写(含已被他人认领)即恢复
			g.mu.Unlock()
			return claimed, nil
		}
		lastErr = err
	}

	// 重试耗尽:跨轮指数退避 + 升级告警(非仅记日志——Hard Rule)
	g.mu.Lock()
	g.writeFailures++
	failures := g.writeFailures
	backoff := g.writeBackoffBase << (failures - 1)
	if backoff > g.writeBackoffMax {
		backoff = g.writeBackoffMax
	}
	g.writeBackoffUntil = g.now().Add(backoff)
	g.mu.Unlock()

	g.logger.Error("持久化日闸写入失败,重试耗尽,升级告警并进入跨轮退避",
		elog.String("resource_type", resourceType),
		elog.Int("consecutive_failures", failures),
		elog.Duration("backoff", backoff),
		elog.FieldErr(lastErr))
	g.alert(ctx, resourceType, "write", failures, lastErr)
	return false, lastErr
}

// alert 上报升级告警(alerter 未装配时仅日志降级——生产装配由 wire 保证)
func (g *PersistentDailyGate) alert(ctx context.Context, resourceType, operation string, failures int, err error) {
	if g.alerter == nil {
		return
	}
	g.alerter.AlertDailyGateFailure(ctx, resourceType, operation, failures, err)
}
