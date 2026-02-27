// Package service 审计模块业务逻辑层
package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/gotomicro/ego/core/elog"
)

// AuditService 审计日志查询服务
type AuditService struct {
	dao    dao.AuditLogDAO
	logger *elog.Component
}

// NewAuditService 创建审计日志查询服务
func NewAuditService(dao dao.AuditLogDAO, logger *elog.Component) *AuditService {
	return &AuditService{dao: dao, logger: logger}
}

// ListAuditLogs 查询审计日志列表
func (s *AuditService) ListAuditLogs(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLog, int64, error) {
	logs, err := s.dao.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("查询审计日志失败: %w", err)
	}
	total, err := s.dao.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("统计审计日志失败: %w", err)
	}
	return logs, total, nil
}

// ExportAuditLogs 导出审计日志
func (s *AuditService) ExportAuditLogs(ctx context.Context, filter domain.AuditLogFilter, format string) ([]byte, error) {
	// 导出时不分页
	filter.Offset = 0
	filter.Limit = 10000

	logs, err := s.dao.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("查询审计日志失败: %w", err)
	}

	switch format {
	case "csv":
		return s.exportCSV(logs)
	default:
		return json.Marshal(logs)
	}
}

func (s *AuditService) exportCSV(logs []domain.AuditLog) ([]byte, error) {
	var buf bytes.Buffer
	// 写入 UTF-8 BOM
	buf.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(&buf)
	// 表头
	_ = w.Write([]string{"ID", "操作类型", "操作人ID", "操作人", "租户ID", "HTTP方法", "API路径", "状态码", "结果", "请求ID", "耗时(ms)", "客户端IP", "时间"})

	for _, log := range logs {
		_ = w.Write([]string{
			fmt.Sprintf("%d", log.ID),
			string(log.OperationType),
			log.OperatorID,
			log.OperatorName,
			log.TenantID,
			log.HTTPMethod,
			log.APIPath,
			fmt.Sprintf("%d", log.StatusCode),
			string(log.Result),
			log.RequestID,
			fmt.Sprintf("%d", log.DurationMs),
			log.ClientIP,
			time.UnixMilli(log.Ctime).Format("2006-01-02 15:04:05"),
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// GenerateReport 生成审计报告
func (s *AuditService) GenerateReport(ctx context.Context, tenantID string, startTime, endTime int64) (*domain.AuditReport, error) {
	filter := domain.AuditLogFilter{
		TenantID:  tenantID,
		StartTime: &startTime,
		EndTime:   &endTime,
	}

	total, err := s.dao.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("统计总数失败: %w", err)
	}

	resultCounts, err := s.dao.CountByResult(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("按结果统计失败: %w", err)
	}

	opTypeCounts, err := s.dao.CountByOperationType(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("按操作类型统计失败: %w", err)
	}

	methodCounts, err := s.dao.CountByHTTPMethod(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("按HTTP方法统计失败: %w", err)
	}

	topEndpoints, err := s.dao.ListTopEndpoints(ctx, filter, 10)
	if err != nil {
		return nil, fmt.Errorf("获取Top端点失败: %w", err)
	}

	topOperators, err := s.dao.ListTopOperators(ctx, filter, 10)
	if err != nil {
		return nil, fmt.Errorf("获取Top操作人失败: %w", err)
	}

	// 转换操作类型统计
	opsByType := make(map[domain.AuditOperationType]int64, len(opTypeCounts))
	for k, v := range opTypeCounts {
		opsByType[domain.AuditOperationType(k)] = v
	}

	report := &domain.AuditReport{
		StartTime:    startTime,
		EndTime:      endTime,
		TotalOps:     total,
		SuccessOps:   resultCounts[string(domain.AuditResultSuccess)],
		FailedOps:    resultCounts[string(domain.AuditResultFailed)],
		OpsByType:    opsByType,
		OpsByMethod:  methodCounts,
		TopEndpoints: topEndpoints,
		TopOperators: topOperators,
		GeneratedAt:  time.Now().UnixMilli(),
	}
	return report, nil
}
