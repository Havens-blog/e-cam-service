package service

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/gotomicro/ego/core/elog"
)

// DashboardService 仪表盘服务接口
type DashboardService interface {
	// GetOverview 获取资产总览
	GetOverview(ctx context.Context, tenantID string) (*DashboardOverview, error)
	// CountByProvider 按云厂商统计
	CountByProvider(ctx context.Context, tenantID string) ([]dao.GroupCount, error)
	// CountByRegion 按地域统计
	CountByRegion(ctx context.Context, tenantID string) ([]dao.GroupCount, error)
	// CountByAssetType 按资产类型统计
	CountByAssetType(ctx context.Context, tenantID string) ([]dao.GroupCount, error)
	// CountByAccount 按云账号统计
	CountByAccount(ctx context.Context, tenantID string) ([]dao.GroupCount, error)
	// GetExpiringResources 获取即将过期的资源
	GetExpiringResources(ctx context.Context, tenantID string, days int, offset, limit int64) ([]dao.Instance, int64, error)
}

// DashboardOverview 仪表盘总览数据
type DashboardOverview struct {
	Total      int64            `json:"total"`
	ByProvider []dao.GroupCount `json:"by_provider"`
	ByType     []dao.GroupCount `json:"by_type"`
	ByStatus   []dao.GroupCount `json:"by_status"`
}

type dashboardService struct {
	dao    dao.DashboardDAO
	logger *elog.Component
}

// NewDashboardService 创建仪表盘服务
func NewDashboardService(dao dao.DashboardDAO) DashboardService {
	return &dashboardService{
		dao:    dao,
		logger: elog.DefaultLogger,
	}
}

// GetOverview 获取资产总览
func (s *dashboardService) GetOverview(ctx context.Context, tenantID string) (*DashboardOverview, error) {
	total, err := s.dao.GetTotalCount(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	byProvider, err := s.dao.CountByProvider(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	byType, err := s.dao.CountByAssetType(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	byStatus, err := s.dao.CountByStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return &DashboardOverview{
		Total:      total,
		ByProvider: byProvider,
		ByType:     byType,
		ByStatus:   byStatus,
	}, nil
}

// CountByProvider 按云厂商统计
func (s *dashboardService) CountByProvider(ctx context.Context, tenantID string) ([]dao.GroupCount, error) {
	return s.dao.CountByProvider(ctx, tenantID)
}

// CountByRegion 按地域统计
func (s *dashboardService) CountByRegion(ctx context.Context, tenantID string) ([]dao.GroupCount, error) {
	return s.dao.CountByRegion(ctx, tenantID)
}

// CountByAssetType 按资产类型统计
func (s *dashboardService) CountByAssetType(ctx context.Context, tenantID string) ([]dao.GroupCount, error) {
	return s.dao.CountByAssetType(ctx, tenantID)
}

// CountByAccount 按云账号统计
func (s *dashboardService) CountByAccount(ctx context.Context, tenantID string) ([]dao.GroupCount, error) {
	return s.dao.CountByAccountID(ctx, tenantID)
}

// GetExpiringResources 获取即将过期的资源
func (s *dashboardService) GetExpiringResources(ctx context.Context, tenantID string, days int, offset, limit int64) ([]dao.Instance, int64, error) {
	return s.dao.GetExpiringInstances(ctx, tenantID, days, offset, limit)
}
