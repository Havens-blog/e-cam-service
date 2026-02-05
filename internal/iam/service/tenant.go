package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	iamrepo "github.com/Havens-blog/e-cam-service/internal/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

// TenantService tenant service interface
type TenantService interface {
	// CreateTenant creates tenant
	CreateTenant(ctx context.Context, req *domain.CreateTenantRequest) (*domain.Tenant, error)

	// GetTenant gets tenant details
	GetTenant(ctx context.Context, tenantID string) (*domain.Tenant, error)

	// ListTenants lists tenants
	ListTenants(ctx context.Context, filter domain.TenantFilter) ([]*domain.Tenant, int64, error)

	// UpdateTenant updates tenant
	UpdateTenant(ctx context.Context, tenantID string, req *domain.UpdateTenantRequest) error

	// DeleteTenant deletes tenant (soft delete)
	DeleteTenant(ctx context.Context, tenantID string) error

	// GetTenantStats gets tenant statistics
	GetTenantStats(ctx context.Context, tenantID string) (*TenantStats, error)

	// ValidateTenantAccess validates tenant access permission
	ValidateTenantAccess(ctx context.Context, tenantID string, operation string) error
}

// TenantStats tenant statistics
type TenantStats struct {
	TenantID          string     `json:"tenant_id"`
	CloudAccountCount int        `json:"cloud_account_count"`
	UserCount         int        `json:"user_count"`
	UserGroupCount    int        `json:"user_group_count"`
	AssetCount        int        `json:"asset_count"`
	LastSyncTime      *time.Time `json:"last_sync_time,omitempty"`
}

type tenantService struct {
	tenantRepo  iamrepo.TenantRepository
	accountRepo repository.CloudAccountRepository
	userRepo    iamrepo.CloudUserRepository
	groupRepo   iamrepo.UserGroupRepository
	logger      *elog.Component
}

// NewTenantService creates tenant service
func NewTenantService(
	tenantRepo iamrepo.TenantRepository,
	accountRepo repository.CloudAccountRepository,
	userRepo iamrepo.CloudUserRepository,
	groupRepo iamrepo.UserGroupRepository,
	logger *elog.Component,
) TenantService {
	return &tenantService{
		tenantRepo:  tenantRepo,
		accountRepo: accountRepo,
		userRepo:    userRepo,
		groupRepo:   groupRepo,
		logger:      logger,
	}
}

// CreateTenant creates tenant
func (s *tenantService) CreateTenant(ctx context.Context, req *domain.CreateTenantRequest) (*domain.Tenant, error) {
	s.logger.Info("创建租户",
		elog.String("tenant_id", req.ID),
		elog.String("name", req.Name))

	// Check if tenant ID already exists
	existingTenant, err := s.tenantRepo.GetByID(ctx, req.ID)
	if err == nil && existingTenant.ID != "" {
		return nil, errs.TenantAlreadyExist
	}
	if err != nil && err != mongo.ErrNoDocuments {
		s.logger.Error("检查租户ID失败", elog.FieldErr(err))
		return nil, fmt.Errorf("检查租户ID失败: %w", err)
	}

	// Check if tenant name already exists
	existingTenant, err = s.tenantRepo.GetByName(ctx, req.Name)
	if err == nil && existingTenant.ID != "" {
		return nil, errs.TenantNameAlreadyExist
	}
	if err != nil && err != mongo.ErrNoDocuments {
		s.logger.Error("检查租户名称失败", elog.FieldErr(err))
		return nil, fmt.Errorf("检查租户名称失败: %w", err)
	}

	now := time.Now()
	tenant := domain.Tenant{
		ID:          req.ID,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Status:      domain.TenantStatusActive,
		Settings:    req.Settings,
		Metadata:    req.Metadata,
		CreateTime:  now,
		UpdateTime:  now,
		CTime:       now.Unix(),
		UTime:       now.Unix(),
	}

	// Set default values
	if tenant.DisplayName == "" {
		tenant.DisplayName = tenant.Name
	}
	if tenant.Settings.Features == nil {
		tenant.Settings.Features = make(map[string]bool)
	}
	if tenant.Settings.CustomFields == nil {
		tenant.Settings.CustomFields = make(map[string]string)
	}
	if tenant.Metadata.Tags == nil {
		tenant.Metadata.Tags = make(map[string]string)
	}

	if err := tenant.Validate(); err != nil {
		s.logger.Error("租户数据验证失败", elog.FieldErr(err))
		return nil, errs.ParamsError
	}

	if err := s.tenantRepo.Create(ctx, tenant); err != nil {
		s.logger.Error("创建租户失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建租户失败: %w", err)
	}

	s.logger.Info("创建租户成功",
		elog.String("tenant_id", tenant.ID),
		elog.String("name", tenant.Name))

	return &tenant, nil
}

// GetTenant gets tenant details
func (s *tenantService) GetTenant(ctx context.Context, tenantID string) (*domain.Tenant, error) {
	s.logger.Debug("获取租户详情", elog.String("tenant_id", tenantID))

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.TenantNotFound
		}
		s.logger.Error("获取租户失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		return nil, fmt.Errorf("获取租户失败: %w", err)
	}

	// Get statistics
	stats, err := s.getTenantStatsInternal(ctx, tenantID)
	if err != nil {
		s.logger.Warn("获取租户统计信息失败", elog.FieldErr(err))
	} else {
		tenant.Metadata.CloudAccountCount = stats.CloudAccountCount
		tenant.Metadata.UserCount = stats.UserCount
		tenant.Metadata.UserGroupCount = stats.UserGroupCount
	}

	return &tenant, nil
}

// ListTenants lists tenants
func (s *tenantService) ListTenants(ctx context.Context, filter domain.TenantFilter) ([]*domain.Tenant, int64, error) {
	s.logger.Debug("获取租户列表",
		elog.String("keyword", filter.Keyword),
		elog.String("status", string(filter.Status)))

	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	tenants, total, err := s.tenantRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取租户列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取租户列表失败: %w", err)
	}

	tenantPtrs := make([]*domain.Tenant, len(tenants))
	for i := range tenants {
		tenantPtrs[i] = &tenants[i]
		// Get statistics
		stats, err := s.getTenantStatsInternal(ctx, tenants[i].ID)
		if err == nil {
			tenantPtrs[i].Metadata.CloudAccountCount = stats.CloudAccountCount
			tenantPtrs[i].Metadata.UserCount = stats.UserCount
			tenantPtrs[i].Metadata.UserGroupCount = stats.UserGroupCount
		}
	}

	s.logger.Debug("获取租户列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(tenants)))

	return tenantPtrs, total, nil
}

// UpdateTenant updates tenant
func (s *tenantService) UpdateTenant(ctx context.Context, tenantID string, req *domain.UpdateTenantRequest) error {
	s.logger.Info("更新租户", elog.String("tenant_id", tenantID))

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.TenantNotFound
		}
		s.logger.Error("获取租户失败", elog.FieldErr(err))
		return fmt.Errorf("获取租户失败: %w", err)
	}

	updated := false
	if req.Name != nil && *req.Name != tenant.Name {
		// Check if new name already exists
		existingTenant, err := s.tenantRepo.GetByName(ctx, *req.Name)
		if err == nil && existingTenant.ID != tenantID {
			return errs.TenantNameAlreadyExist
		}
		if err != nil && err != mongo.ErrNoDocuments {
			s.logger.Error("检查租户名称失败", elog.FieldErr(err))
			return fmt.Errorf("检查租户名称失败: %w", err)
		}
		tenant.Name = *req.Name
		updated = true
	}
	if req.DisplayName != nil && *req.DisplayName != tenant.DisplayName {
		tenant.DisplayName = *req.DisplayName
		updated = true
	}
	if req.Description != nil && *req.Description != tenant.Description {
		tenant.Description = *req.Description
		updated = true
	}
	if req.Status != nil && *req.Status != tenant.Status {
		tenant.Status = *req.Status
		updated = true
	}
	if req.Settings != nil {
		tenant.Settings = *req.Settings
		updated = true
	}
	if req.Metadata != nil {
		tenant.Metadata = *req.Metadata
		updated = true
	}

	if !updated {
		s.logger.Debug("租户信息无变更", elog.String("tenant_id", tenantID))
		return nil
	}

	now := time.Now()
	tenant.UpdateTime = now
	tenant.UTime = now.Unix()

	if err := s.tenantRepo.Update(ctx, tenant); err != nil {
		s.logger.Error("更新租户失败", elog.FieldErr(err))
		return fmt.Errorf("更新租户失败: %w", err)
	}

	s.logger.Info("更新租户成功", elog.String("tenant_id", tenantID))

	return nil
}

// DeleteTenant deletes tenant (soft delete)
func (s *tenantService) DeleteTenant(ctx context.Context, tenantID string) error {
	s.logger.Info("删除租户", elog.String("tenant_id", tenantID))

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.TenantNotFound
		}
		s.logger.Error("获取租户失败", elog.FieldErr(err))
		return fmt.Errorf("获取租户失败: %w", err)
	}

	// Check if tenant has associated resources
	stats, err := s.getTenantStatsInternal(ctx, tenantID)
	if err != nil {
		s.logger.Error("获取租户统计信息失败", elog.FieldErr(err))
		return fmt.Errorf("获取租户统计信息失败: %w", err)
	}

	if stats.CloudAccountCount > 0 || stats.UserCount > 0 || stats.UserGroupCount > 0 {
		return errs.TenantHasResources
	}

	// Soft delete
	tenant.Status = domain.TenantStatusDeleted
	now := time.Now()
	tenant.UpdateTime = now
	tenant.UTime = now.Unix()

	if err := s.tenantRepo.Update(ctx, tenant); err != nil {
		s.logger.Error("删除租户失败", elog.FieldErr(err))
		return fmt.Errorf("删除租户失败: %w", err)
	}

	s.logger.Info("删除租户成功", elog.String("tenant_id", tenantID))

	return nil
}

// GetTenantStats gets tenant statistics
func (s *tenantService) GetTenantStats(ctx context.Context, tenantID string) (*TenantStats, error) {
	s.logger.Debug("获取租户统计信息", elog.String("tenant_id", tenantID))

	return s.getTenantStatsInternal(ctx, tenantID)
}

// ValidateTenantAccess validates tenant access permission
func (s *tenantService) ValidateTenantAccess(ctx context.Context, tenantID string, operation string) error {
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.TenantNotFound
		}
		return fmt.Errorf("获取租户失败: %w", err)
	}

	if !tenant.IsActive() {
		return errs.TenantNotActive
	}

	// Check operation-specific permissions
	switch operation {
	case "create_cloud_account":
		if !tenant.CanCreateCloudAccount() {
			return errs.TenantCloudAccountLimitExceeded
		}
	case "create_user":
		if !tenant.CanCreateUser() {
			return errs.TenantUserLimitExceeded
		}
	case "create_user_group":
		if !tenant.CanCreateUserGroup() {
			return errs.TenantUserGroupLimitExceeded
		}
	}

	return nil
}

// getTenantStatsInternal internal method to get tenant statistics
func (s *tenantService) getTenantStatsInternal(ctx context.Context, tenantID string) (*TenantStats, error) {
	stats := &TenantStats{
		TenantID: tenantID,
	}

	// Count cloud accounts
	accountFilter := domain.CloudAccountFilter{
		TenantID: tenantID,
		Limit:    1,
	}
	_, cloudAccountCount, err := s.accountRepo.List(ctx, accountFilter)
	if err != nil {
		s.logger.Warn("统计云账号数量失败", elog.FieldErr(err))
	} else {
		stats.CloudAccountCount = int(cloudAccountCount)
	}

	// Count users
	userFilter := domain.CloudUserFilter{
		TenantID: tenantID,
		Limit:    1,
	}
	_, userCount, err := s.userRepo.List(ctx, userFilter)
	if err != nil {
		s.logger.Warn("统计用户数量失败", elog.FieldErr(err))
	} else {
		stats.UserCount = int(userCount)
	}

	// Count user groups
	groupFilter := domain.UserGroupFilter{
		TenantID: tenantID,
		Limit:    1,
	}
	_, userGroupCount, err := s.groupRepo.List(ctx, groupFilter)
	if err != nil {
		s.logger.Warn("统计用户组数量失败", elog.FieldErr(err))
	} else {
		stats.UserGroupCount = int(userGroupCount)
	}

	return stats, nil
}
