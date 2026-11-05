package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
)

// ChangeOrderAuditService 变更单维度审计流水服务（任务 7.2）。
//
// 写入仅追加（Hard Rule），经 DAO 唯一稀疏索引承载 DedupKey 幂等去重；
// 查询按单号 at 升序稳定返回（5.11 GET /changes/:id/audit 契约）。
// 孤儿清理（orphan_cleanup）/验证窗口（verify）事件附带结构化载荷，
// 同时作为 5.9/5.10 报告存档的读侧数据源。
type ChangeOrderAuditService struct {
	dao    dao.ChangeOrderAuditDAO
	logger *elog.Component

	// events 统一审计事件写入端口（旁路双写，失败仅记日志）。
	events auditlog.Writer
}

// NewChangeOrderAuditService 创建变更单审计流水服务。
func NewChangeOrderAuditService(dao dao.ChangeOrderAuditDAO, logger *elog.Component, events auditlog.Writer) *ChangeOrderAuditService {
	return &ChangeOrderAuditService{dao: dao, logger: logger, events: events}
}

// Record 追加一条普通审计事件（无去重键）。写入失败记日志并返回错误，
// 由调用方决定是否阻塞主流程（cert 域端口契约：审计失败不阻塞业务）。
func (s *ChangeOrderAuditService) Record(ctx context.Context, entry domain.ChangeOrderAuditEntry) error {
	_, _, err := s.dao.Append(ctx, entry)
	if err != nil {
		s.logger.Warn("变更审计写入失败",
			elog.FieldErr(err),
			elog.String("order_id", entry.OrderID),
			elog.String("action", entry.Action),
		)
		return fmt.Errorf("变更审计写入失败: %w", err)
	}
	s.emitUnifiedEvent(ctx, &entry)
	return nil
}

// RecordDedup 追加带去重键事件：同 (orderID, action, dedupKey) 已存在时
// 返回 inserted=false（幂等契约——5.9/5.10 据此抑制重复告警/重复存档）。
func (s *ChangeOrderAuditService) RecordDedup(ctx context.Context, entry domain.ChangeOrderAuditEntry) (bool, error) {
	_, inserted, err := s.dao.Append(ctx, entry)
	if err != nil {
		s.logger.Warn("变更审计写入失败",
			elog.FieldErr(err),
			elog.String("order_id", entry.OrderID),
			elog.String("action", entry.Action),
		)
		return false, fmt.Errorf("变更审计写入失败: %w", err)
	}
	if inserted {
		// 幂等命中（inserted=false）不重复双写，统一集合与旧存储口径一致。
		s.emitUnifiedEvent(ctx, &entry)
	}
	return inserted, nil
}

// toChangeEventFromOrderEntry 把变更单审计条目映射为统一审计事件（纯函数，表驱动测试）。
//
// 口径：Domain=change；Actor 按 entry.Actor 语义（executor/scheduler 前缀识别为
// 对应系统类型，其余视为 EIAM 人工账号=human）；Result 由 Success 三态映射
// （nil=生命周期动作本身成功）；DedupKey 透传（两套存储的幂等键一致）。
// 参数用指针：ChangeOrderAuditEntry 约 192 字节，按值传递被 gocritic hugeParam 判违规。
func toChangeEventFromOrderEntry(e *domain.ChangeOrderAuditEntry) auditlog.AuditEvent {
	actorType := auditlog.ActorTypeHuman
	switch {
	case strings.HasPrefix(e.Actor, string(auditlog.ActorTypeExecutor)):
		actorType = auditlog.ActorTypeExecutor
	case strings.HasPrefix(e.Actor, string(auditlog.ActorTypeScheduler)):
		actorType = auditlog.ActorTypeScheduler
	}
	result := auditlog.ResultSuccess
	if e.Success != nil && !*e.Success {
		result = auditlog.ResultFailed
	}
	detail := map[string]any{"detail": e.Detail}
	if e.ItemID != "" {
		detail["item_id"] = e.ItemID
	}
	if e.Cloud != "" {
		detail["cloud"] = e.Cloud
	}
	if e.CloudCertID != "" {
		detail["cloud_cert_id"] = e.CloudCertID
	}
	if e.OrphanAction != "" {
		detail["orphan_action"] = e.OrphanAction
	}
	if len(e.UnmetDomains) > 0 {
		detail["unmet_domains"] = e.UnmetDomains
	}
	return auditlog.AuditEvent{
		ActorType:    actorType,
		ActorID:      e.Actor,
		At:           e.At,
		Service:      auditlog.ServiceCAM,
		Domain:       auditlog.DomainChange,
		Action:       e.Action,
		ResourceType: "change_order",
		ResourceID:   e.OrderID,
		Result:       result,
		Detail:       detail,
		DedupKey:     e.DedupKey,
	}
}

// emitUnifiedEvent 旁路写统一事件：Validate + Sanitize + Emit，失败仅记日志。
// TenantID 不在旧条目里（变更单审计按单号全局组织），统一事件置 0 由
// Validate 的租户可空约定放行（跨租户检索时以 service/domain 维度过滤）。
func (s *ChangeOrderAuditService) emitUnifiedEvent(ctx context.Context, entry *domain.ChangeOrderAuditEntry) {
	if s.events == nil {
		return
	}
	ev := toChangeEventFromOrderEntry(entry)
	if err := ev.Validate(); err != nil {
		s.logger.Warn("统一变更单事件骨架校验失败，跳过",
			elog.FieldErr(err),
			elog.String("order_id", entry.OrderID),
			elog.String("action", entry.Action),
		)
		return
	}
	ev.Sanitize()
	if _, err := json.Marshal(ev.Detail); err != nil {
		s.logger.Warn("统一变更单事件序列化失败，跳过", elog.FieldErr(err))
		return
	}
	if err := s.events.Emit(ctx, &ev); err != nil {
		s.logger.Warn("统一变更单事件写入失败（不影响旧存储）",
			elog.FieldErr(err),
			elog.String("order_id", entry.OrderID),
		)
	}
}

// ListByOrder 按单号查询审计流水（at 升序稳定返回）。
func (s *ChangeOrderAuditService) ListByOrder(ctx context.Context, orderID string) ([]domain.ChangeOrderAuditEntry, error) {
	entries, err := s.dao.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("查询变更审计失败: %w", err)
	}
	return entries, nil
}

// ListOrphanCleanupResults 按单号查询孤儿清理结果载荷（action=orphan_cleanup，
// at 升序；ChangeReport.OrphanCleanup 投影）。
func (s *ChangeOrderAuditService) ListOrphanCleanupResults(ctx context.Context, orderID string) ([]domain.ChangeOrderAuditEntry, error) {
	entries, err := s.dao.ListByOrderAction(ctx, orderID, "orphan_cleanup")
	if err != nil {
		return nil, fmt.Errorf("查询孤儿清理存档失败: %w", err)
	}
	return entries, nil
}

// ListUnmetDomains 按单号查询窗口关闭未达标域名清单（action=verify 载荷；
// 取最近一条非空存档——终局判定固化结果，查询期不重算防探测漂移）。
func (s *ChangeOrderAuditService) ListUnmetDomains(ctx context.Context, orderID string) ([]string, error) {
	entries, err := s.dao.ListByOrderAction(ctx, orderID, "verify")
	if err != nil {
		return nil, fmt.Errorf("查询未达标清单失败: %w", err)
	}
	// ListByOrderAction 为 at 升序，自尾向前取最近一条非空清单。
	for i := len(entries) - 1; i >= 0; i-- {
		if len(entries[i].UnmetDomains) > 0 {
			return entries[i].UnmetDomains, nil
		}
	}
	return []string{}, nil
}
