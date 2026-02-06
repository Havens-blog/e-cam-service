// Package service 资产服务层
package service

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/asset/domain"
	"github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
	"github.com/gotomicro/ego/core/elog"
)

// InstanceService 资产实例服务接口
type InstanceService interface {
	Create(ctx context.Context, instance *domain.Instance) (int64, error)
	CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error)
	Update(ctx context.Context, instance *domain.Instance) error
	GetByID(ctx context.Context, id int64) (*domain.Instance, error)
	GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (*domain.Instance, error)
	List(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByAccountID(ctx context.Context, accountID int64) error
	Upsert(ctx context.Context, instance *domain.Instance) error
	Search(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error)
}

type instanceService struct {
	repo   repository.InstanceRepository
	logger *elog.Component
}

func NewInstanceService(repo repository.InstanceRepository, logger *elog.Component) InstanceService {
	if logger == nil {
		logger = elog.Load("default").Build()
	}
	return &instanceService{repo: repo, logger: logger}
}

func (s *instanceService) Create(ctx context.Context, instance *domain.Instance) (int64, error) {
	if err := instance.Validate(); err != nil {
		return 0, err
	}
	id, err := s.repo.Create(ctx, *instance)
	if err != nil {
		s.logger.Error("failed to create instance", elog.FieldErr(err))
		return 0, errs.SystemError
	}
	return id, nil
}

func (s *instanceService) CreateBatch(ctx context.Context, instances []domain.Instance) (int64, error) {
	for _, inst := range instances {
		if err := inst.Validate(); err != nil {
			return 0, err
		}
	}
	count, err := s.repo.CreateBatch(ctx, instances)
	if err != nil {
		s.logger.Error("failed to create instances batch", elog.FieldErr(err))
		return 0, errs.SystemError
	}
	return count, nil
}

func (s *instanceService) Update(ctx context.Context, instance *domain.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if err := s.repo.Update(ctx, *instance); err != nil {
		s.logger.Error("failed to update instance", elog.FieldErr(err), elog.Int64("id", instance.ID))
		return errs.SystemError
	}
	return nil
}

func (s *instanceService) GetByID(ctx context.Context, id int64) (*domain.Instance, error) {
	instance, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("failed to get instance", elog.FieldErr(err), elog.Int64("id", id))
		return nil, errs.InstanceNotFound
	}
	if instance.ID == 0 {
		return nil, errs.InstanceNotFound
	}
	return &instance, nil
}

func (s *instanceService) GetByAssetID(ctx context.Context, tenantID, modelUID, assetID string) (*domain.Instance, error) {
	instance, err := s.repo.GetByAssetID(ctx, tenantID, modelUID, assetID)
	if err != nil {
		s.logger.Error("failed to get instance by asset id", elog.FieldErr(err), elog.String("asset_id", assetID))
		return nil, errs.InstanceNotFound
	}
	if instance.ID == 0 {
		return nil, errs.InstanceNotFound
	}
	return &instance, nil
}

func (s *instanceService) List(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	instances, err := s.repo.List(ctx, filter)
	if err != nil {
		s.logger.Error("failed to list instances", elog.FieldErr(err))
		return nil, 0, errs.SystemError
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		s.logger.Error("failed to count instances", elog.FieldErr(err))
		return nil, 0, errs.SystemError
	}
	return instances, total, nil
}

func (s *instanceService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		s.logger.Error("failed to delete instance", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}
	return nil
}

func (s *instanceService) DeleteByAccountID(ctx context.Context, accountID int64) error {
	if err := s.repo.DeleteByAccountID(ctx, accountID); err != nil {
		s.logger.Error("failed to delete instances by account", elog.FieldErr(err), elog.Int64("account_id", accountID))
		return errs.SystemError
	}
	return nil
}

func (s *instanceService) Upsert(ctx context.Context, instance *domain.Instance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, *instance); err != nil {
		s.logger.Error("failed to upsert instance", elog.FieldErr(err), elog.String("asset_id", instance.AssetID))
		return errs.SystemError
	}
	return nil
}

func (s *instanceService) Search(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	return s.repo.Search(ctx, filter)
}
