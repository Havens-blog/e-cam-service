package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/gotomicro/ego/core/elog"
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
}

// NewChangeOrderAuditService 创建变更单审计流水服务。
func NewChangeOrderAuditService(dao dao.ChangeOrderAuditDAO, logger *elog.Component) *ChangeOrderAuditService {
	return &ChangeOrderAuditService{dao: dao, logger: logger}
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
	return inserted, nil
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
