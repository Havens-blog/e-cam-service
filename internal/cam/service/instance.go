package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/gotomicro/ego/core/elog"
)

// InstanceService 资产实例服务接口
type InstanceService interface {
	// Create 创建实例
	Create(ctx context.Context, instance domain.Instance) (int64, error)
	// CreateBatch 批量创建实例
	CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error)
	// Update 更新实例
	Update(ctx context.Context, instance domain.Instance) error
	// GetByID 根据ID获取实例
	GetByID(ctx context.Context, id int64) (domain.Instance, error)
	// GetByAssetID 根据云厂商资产ID获取实例
	GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (domain.Instance, error)
	// List 获取实例列表
	List(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error)
	// Delete 删除实例
	Delete(ctx context.Context, id int64) error
	// DeleteByAccountID 删除指定云账号的所有实例
	DeleteByAccountID(ctx context.Context, accountID int64) error
	// Upsert 更新或插入实例
	Upsert(ctx context.Context, instance domain.Instance) error
	// UpsertBatch 批量更新或插入实例
	UpsertBatch(ctx context.Context, instances []domain.Instance) error
	// Search 统一搜索实例
	Search(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error)
}

type instanceService struct {
	repo   repository.InstanceRepository
	logger *elog.Component
}

// NewInstanceService 创建实例服务
func NewInstanceService(repo repository.InstanceRepository) InstanceService {
	return &instanceService{
		repo:   repo,
		logger: elog.DefaultLogger,
	}
}

// Create 创建实例
func (s *instanceService) Create(ctx context.Context, instance domain.Instance) (int64, error) {
	if err := instance.Validate(); err != nil {
		return 0, err
	}

	// 检查是否已存在
	existing, err := s.repo.GetByAssetID(ctx, instance.TenantID, instance.ModelUID, instance.AssetID)
	if err != nil {
		return 0, fmt.Errorf("failed to check existing instance: %w", err)
	}
	if existing.ID > 0 {
		return 0, errs.ErrInstanceExists
	}

	return s.repo.Create(ctx, instance)
}

// CreateBatch 批量创建实例
func (s *instanceService) CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error) {
	if len(instances) == 0 {
		return 0, nil
	}

	for _, inst := range instances {
		if err := inst.Validate(); err != nil {
			return 0, err
		}
	}

	return s.repo.CreateBatch(ctx, instances)
}

// Update 更新实例
func (s *instanceService) Update(ctx context.Context, instance domain.Instance) error {
	if instance.ID == 0 {
		return errs.ErrInstanceNotFound
	}

	existing, err := s.repo.GetByID(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}
	if existing.ID == 0 {
		return errs.ErrInstanceNotFound
	}

	return s.repo.Update(ctx, instance)
}

// GetByID 根据ID获取实例
func (s *instanceService) GetByID(ctx context.Context, id int64) (domain.Instance, error) {
	instance, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.Instance{}, fmt.Errorf("failed to get instance: %w", err)
	}
	if instance.ID == 0 {
		return domain.Instance{}, errs.ErrInstanceNotFound
	}
	return instance, nil
}

// GetByAssetID 根据云厂商资产ID获取实例
func (s *instanceService) GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (domain.Instance, error) {
	return s.repo.GetByAssetID(ctx, tenantID, modelUID, assetID)
}

// List 获取实例列表
func (s *instanceService) List(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error) {
	instances, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list instances: %w", err)
	}

	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count instances: %w", err)
	}

	return instances, total, nil
}

// Delete 删除实例
func (s *instanceService) Delete(ctx context.Context, id int64) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}
	if existing.ID == 0 {
		return errs.ErrInstanceNotFound
	}

	return s.repo.Delete(ctx, id)
}

// DeleteByAccountID 删除指定云账号的所有实例
func (s *instanceService) DeleteByAccountID(ctx context.Context, accountID int64) error {
	return s.repo.DeleteByAccountID(ctx, accountID)
}

// Upsert 更新或插入实例
func (s *instanceService) Upsert(ctx context.Context, instance domain.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	return s.repo.Upsert(ctx, instance)
}

// UpsertBatch 批量更新或插入实例
func (s *instanceService) UpsertBatch(ctx context.Context, instances []domain.Instance) error {
	for _, inst := range instances {
		if err := inst.Validate(); err != nil {
			return err
		}
		if err := s.repo.Upsert(ctx, inst); err != nil {
			s.logger.Error("failed to upsert instance",
				elog.String("asset_id", inst.AssetID),
				elog.String("model_uid", inst.ModelUID),
				elog.FieldErr(err),
			)
			return err
		}
	}
	return nil
}

// Search 统一搜索实例
func (s *instanceService) Search(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error) {
	return s.repo.Search(ctx, filter)
}
