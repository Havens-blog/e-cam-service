// Package cert 证书管理功能域。
//
// 本文件为 cert 域审计桥（任务 7.2）：5.8 RollbackAuditRecorder、
// 5.9 OrphanCleanupRecorder、5.10 VerifyWindowRecorder、5.11 读端口
// （ChangeAuditSource/ChangeUnmetSource/ChangeOrphanCleanupSource）与
// 7.2 ChangeAuditWriter 的统一生产实现——写入/查询全部经 cert 中性审计存储
// 端口 service.ChangeAuditStore（单集合仅追加，Hard Rule：无 update/delete
// 路径）。本桥对 internal/audit 零 import：存储实现由组合根用 internal/audit
// ChangeOrderAuditService 适配并注入（审计数据归属推迟到接口背后，为 cert
// 服务抽取清除出站耦合）。
package cert

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// changeAuditBridge cert 域审计桥：实现 service 层全部审计写入/读取端口。
// 审计失败不阻塞业务主流程（错误上抛由调用方按端口契约处置；本桥负责
// 日志告警——存储实现层已记 Warn，此处不再重复记）。
type changeAuditBridge struct {
	store service.ChangeAuditStore
}

// 编译期断言：审计桥实现 cert 域全部审计端口。
var (
	_ service.ChangeAuditWriter         = (*changeAuditBridge)(nil)
	_ service.RollbackAuditRecorder     = (*changeAuditBridge)(nil)
	_ service.OrphanCleanupRecorder     = (*changeAuditBridge)(nil)
	_ service.VerifyWindowRecorder      = (*changeAuditBridge)(nil)
	_ service.ChangeAuditSource         = (*changeAuditBridge)(nil)
	_ service.ChangeUnmetSource         = (*changeAuditBridge)(nil)
	_ service.ChangeOrphanCleanupSource = (*changeAuditBridge)(nil)
)

// newChangeAuditBridge 构造审计桥：接收组合根已装配的变更单审计存储端口
// （DAO 构造 + 索引初始化属持久化装配，置于组合根——见 ioc/cert.go；
// 本领域包仅依赖 cert 中性存储端口 service.ChangeAuditStore，不 import
// internal/audit 的 domain/service）。
func newChangeAuditBridge(store service.ChangeAuditStore) *changeAuditBridge {
	return &changeAuditBridge{store: store}
}

// ---- service.ChangeAuditWriter（web 订单生命周期 + 执行引擎 item_result）----

// WriteChangeAudit 追加订单审计事件（At 零值补当前时间）。
func (b *changeAuditBridge) WriteChangeAudit(ctx context.Context, e service.ChangeAuditEvent) error {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	return b.store.Record(ctx, service.ChangeAuditEntry{
		OrderID: e.OrderID,
		ItemID:  e.ItemID,
		Actor:   e.Actor,
		Action:  e.Action,
		Detail:  e.Detail,
		At:      at.UnixMilli(),
	})
}

// ---- service.RollbackAuditRecorder（5.8 回滚事件 → action=rollback）----

// RecordRollback 回滚审计事件映射：Outcome 并入 Detail（机器可读），
// actor 取 ctx 操作者（HTTP 触发回滚归因），缺省系统标识。
func (b *changeAuditBridge) RecordRollback(ctx context.Context, event service.RollbackAuditEvent) error {
	detail := event.Outcome
	if event.Detail != "" {
		detail += ": " + event.Detail
	}
	return b.WriteChangeAudit(ctx, service.ChangeAuditEvent{
		OrderID: event.OrderID,
		ItemID:  event.ItemID,
		Actor:   actorOrDefault(service.OperatorFromContext(ctx), service.ActorScheduler),
		Action:  service.AuditActionRollback,
		Detail:  detail,
		At:      event.At,
	})
}

// ---- service.OrphanCleanupRecorder（5.9 → action=orphan_cleanup + 报告存档）----

// RecordOrphanCleanup 孤儿清理结果落审计（结构化载荷 + 去重键
// (cloudCertID, action, success)——重复记录返回 false，调用方抑制重复告警）。
func (b *changeAuditBridge) RecordOrphanCleanup(ctx context.Context, orderID string, result service.OrphanCleanupResult) (bool, error) {
	at := result.At
	if at.IsZero() {
		at = time.Now()
	}
	success := result.Success
	return b.store.RecordDedup(ctx, service.ChangeAuditEntry{
		OrderID: orderID,
		Actor:   actorOrDefault(service.OperatorFromContext(ctx), service.ActorScheduler),
		Action:  service.AuditActionOrphanCleanup,
		Detail: fmt.Sprintf("cloud=%s cloudCertId=%s action=%s success=%t",
			result.Cloud, result.CloudCertID, result.Action, result.Success),
		At:           at.UnixMilli(),
		Cloud:        result.Cloud,
		CloudCertID:  result.CloudCertID,
		OrphanAction: result.Action,
		Success:      &success,
		DedupKey:     fmt.Sprintf("%s|%s|%t", result.CloudCertID, result.Action, result.Success),
	})
}

// ---- service.VerifyWindowRecorder（5.10 → action=verify + 未达标存档）----

// RecordUnmetDomains 窗口关闭未达标清单落审计（payload=UnmetDomains，
// 去重键=at；ChangeReport.UnmetDomains 权威来源）。
func (b *changeAuditBridge) RecordUnmetDomains(ctx context.Context, orderID string, unmetDomains []string, at time.Time) (bool, error) {
	return b.store.RecordDedup(ctx, service.ChangeAuditEntry{
		OrderID:      orderID,
		Actor:        actorOrDefault(service.OperatorFromContext(ctx), service.ActorScheduler),
		Action:       service.AuditActionVerify,
		Detail:       fmt.Sprintf("verify window closed with %d unmet domains", len(unmetDomains)),
		At:           at.UnixMilli(),
		UnmetDomains: unmetDomains,
		DedupKey:     strconv.FormatInt(at.UnixMilli(), 10),
	})
}

// ---- service.ChangeAuditSource（5.11 按单查询）----

// ListByOrder 按单号查询审计流水（at 升序稳定返回 → 5.11 端点契约）。
func (b *changeAuditBridge) ListByOrder(ctx context.Context, orderID string) ([]service.ChangeAuditLog, error) {
	entries, err := b.store.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	logs := make([]service.ChangeAuditLog, 0, len(entries))
	for _, e := range entries {
		logs = append(logs, service.ChangeAuditLog{
			At:     time.UnixMilli(e.At),
			Actor:  e.Actor,
			Action: e.Action,
			Detail: e.Detail,
			ItemID: e.ItemID,
		})
	}
	return logs, nil
}

// ---- service.ChangeUnmetSource（5.11 报告聚合读侧）----

// ListUnmetDomains 窗口关闭未达标域名清单（最近一条非空存档）。
func (b *changeAuditBridge) ListUnmetDomains(ctx context.Context, orderID string) ([]string, error) {
	return b.store.ListUnmetDomains(ctx, orderID)
}

// ---- service.ChangeOrphanCleanupSource（5.11 报告聚合读侧）----

// ListOrphanCleanup 按单查询孤儿清理结果（at 升序 → ChangeReport.OrphanCleanup）。
func (b *changeAuditBridge) ListOrphanCleanup(ctx context.Context, orderID string) ([]service.OrphanCleanupResult, error) {
	entries, err := b.store.ListOrphanCleanupResults(ctx, orderID)
	if err != nil {
		return nil, err
	}
	results := make([]service.OrphanCleanupResult, 0, len(entries))
	for _, e := range entries {
		success := e.Success != nil && *e.Success
		results = append(results, service.OrphanCleanupResult{
			Cloud:       e.Cloud,
			CloudCertID: e.CloudCertID,
			Action:      e.OrphanAction,
			Success:     success,
			At:          time.UnixMilli(e.At),
		})
	}
	return results, nil
}

// actorOrDefault actor 空值回退（系统事件标识）。
func actorOrDefault(actor, fallback string) string {
	if actor == "" {
		return fallback
	}
	return actor
}
