package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// AuditLogRepository 审计日志仓储接口
type AuditLogRepository interface {
	// Create 创建审计日志
	Create(ctx context.Context, log domain.AuditLog) (int64, error)

	// GetByID 根据ID获取审计日志
	GetByID(ctx context.Context, id int64) (domain.AuditLog, error)

	// List 获取审计日志列表
	List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLog, int64, error)

	// CountByOperationType 按操作类型统计
	CountByOperationType(ctx context.Context, filter domain.AuditLogFilter) (map[domain.AuditOperationType]int64, error)

	// CountByCloudPlatform 按云平台统计
	CountByCloudPlatform(ctx context.Context, filter domain.AuditLogFilter) (map[domain.CloudProvider]int64, error)

	// CountByResult 按结果统计
	CountByResult(ctx context.Context, filter domain.AuditLogFilter) (map[domain.AuditResult]int64, error)

	// ListTopOperators 获取操作最多的用户列表
	ListTopOperators(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.OperatorStats, error)
}

type auditLogRepository struct {
	dao dao.AuditLogDAO
}

// NewAuditLogRepository 创建审计日志仓储
func NewAuditLogRepository(dao dao.AuditLogDAO) AuditLogRepository {
	return &auditLogRepository{
		dao: dao,
	}
}

// Create 创建审计日志
func (repo *auditLogRepository) Create(ctx context.Context, log domain.AuditLog) (int64, error) {
	daoLog := repo.toEntity(log)
	return repo.dao.Create(ctx, daoLog)
}

// GetByID 根据ID获取审计日志
func (repo *auditLogRepository) GetByID(ctx context.Context, id int64) (domain.AuditLog, error) {
	daoLog, err := repo.dao.GetByID(ctx, id)
	if err != nil {
		return domain.AuditLog{}, err
	}
	return repo.toDomain(daoLog), nil
}

// List 获取审计日志列表
func (repo *auditLogRepository) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLog, int64, error) {
	daoFilter := dao.AuditLogFilter{
		OperationType: dao.AuditOperationType(filter.OperationType),
		OperatorID:    filter.OperatorID,
		TargetType:    filter.TargetType,
		CloudPlatform: dao.CloudProvider(filter.CloudPlatform),
		TenantID:      filter.TenantID,
		StartTime:     filter.StartTime,
		EndTime:       filter.EndTime,
		Offset:        filter.Offset,
		Limit:         filter.Limit,
	}

	daoLogs, err := repo.dao.List(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.Count(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	logs := make([]domain.AuditLog, len(daoLogs))
	for i, daoLog := range daoLogs {
		logs[i] = repo.toDomain(daoLog)
	}

	return logs, count, nil
}

// CountByOperationType 按操作类型统计
func (repo *auditLogRepository) CountByOperationType(ctx context.Context, filter domain.AuditLogFilter) (map[domain.AuditOperationType]int64, error) {
	daoFilter := dao.AuditLogFilter{
		OperationType: dao.AuditOperationType(filter.OperationType),
		OperatorID:    filter.OperatorID,
		TargetType:    filter.TargetType,
		CloudPlatform: dao.CloudProvider(filter.CloudPlatform),
		TenantID:      filter.TenantID,
		StartTime:     filter.StartTime,
		EndTime:       filter.EndTime,
	}

	daoResult, err := repo.dao.CountByOperationType(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	result := make(map[domain.AuditOperationType]int64)
	for opType, count := range daoResult {
		result[domain.AuditOperationType(opType)] = count
	}

	return result, nil
}

// CountByCloudPlatform 按云平台统计
func (repo *auditLogRepository) CountByCloudPlatform(ctx context.Context, filter domain.AuditLogFilter) (map[domain.CloudProvider]int64, error) {
	daoFilter := dao.AuditLogFilter{
		OperationType: dao.AuditOperationType(filter.OperationType),
		OperatorID:    filter.OperatorID,
		TargetType:    filter.TargetType,
		CloudPlatform: dao.CloudProvider(filter.CloudPlatform),
		TenantID:      filter.TenantID,
		StartTime:     filter.StartTime,
		EndTime:       filter.EndTime,
	}

	daoResult, err := repo.dao.CountByCloudPlatform(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	result := make(map[domain.CloudProvider]int64)
	for provider, count := range daoResult {
		result[domain.CloudProvider(provider)] = count
	}

	return result, nil
}

// CountByResult 按结果统计
func (repo *auditLogRepository) CountByResult(ctx context.Context, filter domain.AuditLogFilter) (map[domain.AuditResult]int64, error) {
	daoFilter := dao.AuditLogFilter{
		OperationType: dao.AuditOperationType(filter.OperationType),
		OperatorID:    filter.OperatorID,
		TargetType:    filter.TargetType,
		CloudPlatform: dao.CloudProvider(filter.CloudPlatform),
		TenantID:      filter.TenantID,
		StartTime:     filter.StartTime,
		EndTime:       filter.EndTime,
	}

	daoResult, err := repo.dao.CountByResult(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	result := make(map[domain.AuditResult]int64)
	for auditResult, count := range daoResult {
		result[domain.AuditResult(auditResult)] = count
	}

	return result, nil
}

// ListTopOperators 获取操作最多的用户列表
func (repo *auditLogRepository) ListTopOperators(ctx context.Context, filter domain.AuditLogFilter, limit int) ([]domain.OperatorStats, error) {
	daoFilter := dao.AuditLogFilter{
		OperationType: dao.AuditOperationType(filter.OperationType),
		OperatorID:    filter.OperatorID,
		TargetType:    filter.TargetType,
		CloudPlatform: dao.CloudProvider(filter.CloudPlatform),
		TenantID:      filter.TenantID,
		StartTime:     filter.StartTime,
		EndTime:       filter.EndTime,
	}

	daoStats, err := repo.dao.ListTopOperators(ctx, daoFilter, limit)
	if err != nil {
		return nil, err
	}

	stats := make([]domain.OperatorStats, len(daoStats))
	for i, daoStat := range daoStats {
		stats[i] = domain.OperatorStats{
			OperatorID:   daoStat.OperatorID,
			OperatorName: daoStat.OperatorName,
			OpCount:      daoStat.OpCount,
		}
	}

	return stats, nil
}

// toDomain 转换为领域模型
func (repo *auditLogRepository) toDomain(daoLog dao.AuditLog) domain.AuditLog {
	return domain.AuditLog{
		ID:            daoLog.ID,
		OperationType: domain.AuditOperationType(daoLog.OperationType),
		OperatorID:    daoLog.OperatorID,
		OperatorName:  daoLog.OperatorName,
		TargetType:    daoLog.TargetType,
		TargetID:      daoLog.TargetID,
		TargetName:    daoLog.TargetName,
		CloudPlatform: domain.CloudProvider(daoLog.CloudPlatform),
		BeforeValue:   daoLog.BeforeValue,
		AfterValue:    daoLog.AfterValue,
		Result:        domain.AuditResult(daoLog.Result),
		ErrorMessage:  daoLog.ErrorMessage,
		IPAddress:     daoLog.IPAddress,
		UserAgent:     daoLog.UserAgent,
		TenantID:      daoLog.TenantID,
		CreateTime:    daoLog.CreateTime,
		CTime:         daoLog.CTime,
	}
}

// toEntity 转换为DAO实体
func (repo *auditLogRepository) toEntity(log domain.AuditLog) dao.AuditLog {
	return dao.AuditLog{
		ID:            log.ID,
		OperationType: dao.AuditOperationType(log.OperationType),
		OperatorID:    log.OperatorID,
		OperatorName:  log.OperatorName,
		TargetType:    log.TargetType,
		TargetID:      log.TargetID,
		TargetName:    log.TargetName,
		CloudPlatform: dao.CloudProvider(log.CloudPlatform),
		BeforeValue:   log.BeforeValue,
		AfterValue:    log.AfterValue,
		Result:        dao.AuditResult(log.Result),
		ErrorMessage:  log.ErrorMessage,
		IPAddress:     log.IPAddress,
		UserAgent:     log.UserAgent,
		TenantID:      log.TenantID,
		CreateTime:    log.CreateTime,
		CTime:         log.CTime,
	}
}
