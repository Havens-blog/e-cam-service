// Package service 审计模块业务逻辑层
package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
)

// IAMEventFetcher 联邦读端口（F2）：拉取 e-iam 侧统一审计事件。
// 具体实现为 IAMEventFederationClient（S2S HTTP）；单测以桩替换。
type IAMEventFetcher interface {
	FetchIAMEvents(ctx context.Context, tenantID int64, filter *dao.AuditEventFilter, offset, limit int) ([]auditlog.AuditEvent, int64, error)
}

// AuditEventService 统一审计事件查询服务。
// 跨域（audit/operation/change/…）统一检索的读服务；写侧由各触发点经
// e-common-go/auditlog 的 Writer 旁路写入。
type AuditEventService struct {
	dao       dao.AuditEventDAO
	logger    *elog.Component
	fedClient IAMEventFetcher // 可为 nil（未配置 = 单源 Mongo）
}

// NewAuditEventService 创建统一审计事件查询服务
func NewAuditEventService(eventDAO dao.AuditEventDAO, logger *elog.Component, fedClient IAMEventFetcher) *AuditEventService {
	return &AuditEventService{dao: eventDAO, logger: logger, fedClient: fedClient}
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

// ListEventsFederated 联邦查询：本侧 Mongo + e-iam 侧事件按 at 归并排序，
// 统一分页返回。降级语义：e-iam 不可达时返回本侧单源 + degraded=true
// （调用方据此提示「IAM 侧暂不可用」），绝不因联邦失败丢失本侧数据。
//
// 归并取窗：每源拉 [0, offset+limit)（at 降序），归并后切 [offset, offset+limit)
// —— 深分页时两侧拉取量线性放大，但结果始终正确（审计浏览分页浅，可接受）。
// 返回值第三位为 degraded 标记。
func (s *AuditEventService) ListEventsFederated(ctx context.Context, filter *dao.AuditEventFilter) ([]auditlog.AuditEvent, int64, bool, error) {
	// 本侧：放大取窗（副本 filter，避免污染调用方）
	localFilter := *filter
	localFilter.Offset = 0
	localFilter.Limit = filter.Offset + filter.Limit
	localEvents, err := s.dao.List(ctx, &localFilter)
	if err != nil {
		return nil, 0, false, fmt.Errorf("查询统一审计事件失败: %w", err)
	}
	localTotal, err := s.dao.Count(ctx, filter)
	if err != nil {
		return nil, 0, false, fmt.Errorf("统计统一审计事件失败: %w", err)
	}

	merged := append([]auditlog.AuditEvent{}, localEvents...)
	total := localTotal
	degraded := false

	if s.fedClient != nil && filter.TenantID > 0 {
		iamEvents, iamTotal, fedErr := s.fedClient.FetchIAMEvents(ctx, filter.TenantID, filter, 0, int(filter.Offset+filter.Limit))
		if fedErr != nil {
			// 联邦降级：e-iam 不可达不丢本侧数据，仅标记
			s.logger.Warn("联邦读侧降级（e-iam 不可达），返回本侧单源",
				elog.FieldErr(fedErr),
				elog.Int64("tenant_id", filter.TenantID),
			)
			degraded = true
		} else {
			total += iamTotal
			merged = append(merged, iamEvents...)
		}
	}

	// 归并排序（at 降序）后切页
	sortEventsByAtDesc(merged)
	end := int(filter.Offset) + int(filter.Limit)
	if end > len(merged) {
		end = len(merged)
	}
	page := []auditlog.AuditEvent{}
	if int(filter.Offset) < len(merged) {
		page = merged[int(filter.Offset):end]
	}
	return page, total, degraded, nil
}

// sortEventsByAtDesc 按 at 降序原地排序（归并前的统一排序键）。
func sortEventsByAtDesc(events []auditlog.AuditEvent) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].At > events[j-1].At; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}
