package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	iamrepo "github.com/Havens-blog/e-cam-service/internal/cam/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

type AuditService interface {
	LogAudit(ctx context.Context, log *domain.AuditLog) error
	ListAuditLogs(ctx context.Context, filter domain.AuditLogFilter) ([]*domain.AuditLog, int64, error)
	ExportAuditLogs(ctx context.Context, filter domain.AuditLogFilter, format domain.ExportFormat) ([]byte, error)
	GenerateAuditReport(ctx context.Context, req *domain.AuditReportRequest) (*domain.AuditReport, error)
}

type auditService struct {
	auditRepo iamrepo.AuditLogRepository
	logger    *elog.Component
}

func NewAuditService(
	auditRepo iamrepo.AuditLogRepository,
	logger *elog.Component,
) AuditService {
	return &auditService{
		auditRepo: auditRepo,
		logger:    logger,
	}
}

func (s *auditService) LogAudit(ctx context.Context, log *domain.AuditLog) error {
	s.logger.Debug("记录审计日志",
		elog.String("operation_type", string(log.OperationType)),
		elog.String("operator_id", log.OperatorID),
		elog.String("target_type", log.TargetType))

	if err := s.validateAuditLog(log); err != nil {
		s.logger.Error("审计日志验证失败", elog.FieldErr(err))
		return err
	}

	now := time.Now()
	log.CreateTime = now
	log.CTime = now.Unix()

	id, err := s.auditRepo.Create(ctx, *log)
	if err != nil {
		s.logger.Error("创建审计日志失败", elog.FieldErr(err))
		return fmt.Errorf("创建审计日志失败: %w", err)
	}

	log.ID = id

	s.logger.Debug("记录审计日志成功",
		elog.Int64("audit_id", id),
		elog.String("operation_type", string(log.OperationType)))

	return nil
}

func (s *auditService) ListAuditLogs(ctx context.Context, filter domain.AuditLogFilter) ([]*domain.AuditLog, int64, error) {
	s.logger.Debug("获取审计日志列表",
		elog.String("operation_type", string(filter.OperationType)),
		elog.String("operator_id", filter.OperatorID))

	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	logs, total, err := s.auditRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取审计日志列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取审计日志列表失败: %w", err)
	}

	logPtrs := make([]*domain.AuditLog, len(logs))
	for i := range logs {
		logPtrs[i] = &logs[i]
	}

	s.logger.Debug("获取审计日志列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(logs)))

	return logPtrs, total, nil
}


func (s *auditService) ExportAuditLogs(ctx context.Context, filter domain.AuditLogFilter, format domain.ExportFormat) ([]byte, error) {
	s.logger.Info("导出审计日志",
		elog.String("format", string(format)),
		elog.String("tenant_id", filter.TenantID))

	startTime := time.Now()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter.Limit = 10000

	logs, _, err := s.auditRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取审计日志失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取审计日志失败: %w", err)
	}

	var data []byte
	switch format {
	case domain.ExportFormatCSV:
		data, err = s.exportToCSV(logs)
	case domain.ExportFormatJSON:
		data, err = s.exportToJSON(logs)
	default:
		return nil, fmt.Errorf("不支持的导出格式: %s", format)
	}

	if err != nil {
		s.logger.Error("导出审计日志失败", elog.FieldErr(err))
		return nil, fmt.Errorf("导出审计日志失败: %w", err)
	}

	duration := time.Since(startTime)
	s.logger.Info("导出审计日志成功",
		elog.String("format", string(format)),
		elog.Int("count", len(logs)),
		elog.Duration("duration", duration))

	return data, nil
}

func (s *auditService) GenerateAuditReport(ctx context.Context, req *domain.AuditReportRequest) (*domain.AuditReport, error) {
	s.logger.Info("生成审计报告",
		elog.String("tenant_id", req.TenantID),
		elog.String("start_time", req.StartTime.Format("2006-01-02 15:04:05")),
		elog.String("end_time", req.EndTime.Format("2006-01-02 15:04:05")))

	if err := s.validateReportRequest(req); err != nil {
		s.logger.Error("审计报告请求验证失败", elog.FieldErr(err))
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := domain.AuditLogFilter{
		TenantID:  req.TenantID,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Limit:     100000,
	}

	logs, _, err := s.auditRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取审计日志失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取审计日志失败: %w", err)
	}

	report := &domain.AuditReport{
		StartTime:     *req.StartTime,
		EndTime:       *req.EndTime,
		TotalOps:      int64(len(logs)),
		OpsByType:     make(map[domain.AuditOperationType]int64),
		OpsByPlatform: make(map[domain.CloudProvider]int64),
		TopOperators:  make([]domain.OperatorStats, 0),
		GeneratedAt:   time.Now(),
	}

	operatorMap := make(map[string]*domain.OperatorStats)

	for _, log := range logs {
		if log.Result == domain.AuditResultSuccess {
			report.SuccessOps++
		} else {
			report.FailedOps++
		}

		report.OpsByType[log.OperationType]++

		if log.CloudPlatform != "" {
			report.OpsByPlatform[log.CloudPlatform]++
		}

		if log.OperatorID != "" {
			if stats, exists := operatorMap[log.OperatorID]; exists {
				stats.OpCount++
			} else {
				operatorMap[log.OperatorID] = &domain.OperatorStats{
					OperatorID:   log.OperatorID,
					OperatorName: log.OperatorName,
					OpCount:      1,
				}
			}
		}
	}

	for _, stats := range operatorMap {
		report.TopOperators = append(report.TopOperators, *stats)
	}

	if len(report.TopOperators) > 10 {
		report.TopOperators = s.getTopN(report.TopOperators, 10)
	}

	s.logger.Info("生成审计报告成功",
		elog.Int64("total_ops", report.TotalOps),
		elog.Int64("success_ops", report.SuccessOps),
		elog.Int64("failed_ops", report.FailedOps))

	return report, nil
}

func (s *auditService) validateAuditLog(log *domain.AuditLog) error {
	if log.OperationType == "" {
		return fmt.Errorf("操作类型不能为空")
	}
	if log.OperatorID == "" {
		return fmt.Errorf("操作人ID不能为空")
	}
	if log.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}

	validOperations := map[domain.AuditOperationType]bool{
		domain.AuditOpCreateUser:       true,
		domain.AuditOpUpdateUser:       true,
		domain.AuditOpDeleteUser:       true,
		domain.AuditOpCreateGroup:      true,
		domain.AuditOpUpdateGroup:      true,
		domain.AuditOpDeleteGroup:      true,
		domain.AuditOpAssignPermission: true,
		domain.AuditOpRevokePermission: true,
		domain.AuditOpSyncUser:         true,
		domain.AuditOpSyncPermission:   true,
	}
	if !validOperations[log.OperationType] {
		return fmt.Errorf("无效的操作类型")
	}

	return nil
}

func (s *auditService) validateReportRequest(req *domain.AuditReportRequest) error {
	if req.StartTime == nil {
		return fmt.Errorf("开始时间不能为空")
	}
	if req.EndTime == nil {
		return fmt.Errorf("结束时间不能为空")
	}
	if req.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}
	if req.EndTime.Before(*req.StartTime) {
		return fmt.Errorf("结束时间不能早于开始时间")
	}

	return nil
}

func (s *auditService) exportToCSV(logs []domain.AuditLog) ([]byte, error) {
	var buf strings.Builder
	writer := csv.NewWriter(&buf)

	headers := []string{
		"ID", "操作类型", "操作人ID", "操作人名称", "目标类型", "目标ID", "目标名称",
		"云平台", "变更前", "变更后", "结果", "错误信息", "IP地址", "UserAgent", "创建时间",
	}
	if err := writer.Write(headers); err != nil {
		return nil, fmt.Errorf("写入CSV头失败: %w", err)
	}

	for _, log := range logs {
		record := []string{
			fmt.Sprintf("%d", log.ID),
			string(log.OperationType),
			log.OperatorID,
			log.OperatorName,
			log.TargetType,
			fmt.Sprintf("%d", log.TargetID),
			log.TargetName,
			string(log.CloudPlatform),
			log.BeforeValue,
			log.AfterValue,
			string(log.Result),
			log.ErrorMessage,
			log.IPAddress,
			log.UserAgent,
			log.CreateTime.Format("2006-01-02 15:04:05"),
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("写入CSV记录失败: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("CSV写入错误: %w", err)
	}

	return []byte(buf.String()), nil
}

func (s *auditService) exportToJSON(logs []domain.AuditLog) ([]byte, error) {
	data, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("JSON序列化失败: %w", err)
	}
	return data, nil
}

func (s *auditService) getTopN(operators []domain.OperatorStats, n int) []domain.OperatorStats {
	for i := 0; i < len(operators)-1; i++ {
		for j := i + 1; j < len(operators); j++ {
			if operators[j].OpCount > operators[i].OpCount {
				operators[i], operators[j] = operators[j], operators[i]
			}
		}
	}

	if len(operators) > n {
		return operators[:n]
	}
	return operators
}
