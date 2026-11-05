// Package service 审计模块业务逻辑层
package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/gotomicro/ego/core/elog"
)

// AuditEventService 统一审计事件查询服务。
// 跨域（audit/operation/change/…）统一检索的读服务；写侧由各触发点经
// e-common-go/auditlog 的 Writer 旁路写入。
type AuditEventService struct {
	dao    dao.AuditEventDAO
	logger *elog.Component
}

// NewAuditEventService 创建统一审计事件查询服务
func NewAuditEventService(eventDAO dao.AuditEventDAO, logger *elog.Component) *AuditEventService {
	return &AuditEventService{dao: eventDAO, logger: logger}
}

// ListEvents 跨域查询统一审计事件。
// 租户隔离在 filter 上强制：TenantID 由 handler 从登录上下文注入，
// 服务层不接受调用方自报租户（防跨租户拉取他人审计）。
func (s *AuditEventService) ListEvents(ctx context.Context, filter *dao.AuditEventFilter) ([]auditlog.AuditEvent, int64, error) {
	events, err := s.dao.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("查询统一审计事件失败: %w", err)
	}
	total, err := s.dao.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("统计统一审计事件失败: %w", err)
	}
	return events, total, nil
}
