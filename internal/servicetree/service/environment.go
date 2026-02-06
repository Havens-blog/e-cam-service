package service

import (
	"context"
	"errors"

	"github.com/Havens-blog/e-cam-service/internal/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// EnvironmentService 环境服务接口
type EnvironmentService interface {
	Create(ctx context.Context, env domain.Environment) (int64, error)
	Update(ctx context.Context, env domain.Environment) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (domain.Environment, error)
	GetByCode(ctx context.Context, tenantID, code string) (domain.Environment, error)
	List(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, int64, error)
	InitDefaultEnvs(ctx context.Context, tenantID string) error
}

type environmentService struct {
	envRepo     repository.EnvironmentRepository
	bindingRepo repository.BindingRepository
	logger      *elog.Component
}

// NewEnvironmentService 创建环境服务
func NewEnvironmentService(
	envRepo repository.EnvironmentRepository,
	bindingRepo repository.BindingRepository,
	logger *elog.Component,
) EnvironmentService {
	return &environmentService{
		envRepo:     envRepo,
		bindingRepo: bindingRepo,
		logger:      logger,
	}
}

func (s *environmentService) Create(ctx context.Context, env domain.Environment) (int64, error) {
	if err := env.Validate(); err != nil {
		return 0, err
	}

	_, err := s.envRepo.GetByCode(ctx, env.TenantID, env.Code)
	if err == nil {
		return 0, domain.ErrEnvCodeExists
	}
	if !errors.Is(err, domain.ErrEnvNotFound) {
		return 0, err
	}

	if env.Status == 0 {
		env.Status = domain.EnvStatusEnabled
	}

	id, err := s.envRepo.Create(ctx, env)
	if err != nil {
		return 0, err
	}

	s.logger.Info("创建环境成功", elog.Int64("envID", id), elog.String("code", env.Code))
	return id, nil
}

func (s *environmentService) Update(ctx context.Context, env domain.Environment) error {
	existing, err := s.envRepo.GetByID(ctx, env.ID)
	if err != nil {
		return err
	}

	if env.Code != existing.Code {
		_, err := s.envRepo.GetByCode(ctx, env.TenantID, env.Code)
		if err == nil {
			return domain.ErrEnvCodeExists
		}
		if !errors.Is(err, domain.ErrEnvNotFound) {
			return err
		}
	}

	env.TenantID = existing.TenantID
	return s.envRepo.Update(ctx, env)
}

func (s *environmentService) Delete(ctx context.Context, id int64) error {
	count, err := s.bindingRepo.Count(ctx, domain.BindingFilter{EnvID: id})
	if err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrEnvHasBindings
	}

	return s.envRepo.Delete(ctx, id)
}

func (s *environmentService) GetByID(ctx context.Context, id int64) (domain.Environment, error) {
	return s.envRepo.GetByID(ctx, id)
}

func (s *environmentService) GetByCode(ctx context.Context, tenantID, code string) (domain.Environment, error) {
	return s.envRepo.GetByCode(ctx, tenantID, code)
}

func (s *environmentService) List(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, int64, error) {
	envs, err := s.envRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.envRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return envs, total, nil
}

func (s *environmentService) InitDefaultEnvs(ctx context.Context, tenantID string) error {
	count, err := s.envRepo.Count(ctx, domain.EnvironmentFilter{TenantID: tenantID})
	if err != nil {
		return err
	}
	if count > 0 {
		s.logger.Info("租户已有环境，跳过初始化", elog.String("tenantID", tenantID))
		return nil
	}

	defaultEnvs := domain.DefaultEnvironments(tenantID)
	_, err = s.envRepo.CreateBatch(ctx, defaultEnvs)
	if err != nil {
		return err
	}

	s.logger.Info("初始化默认环境成功", elog.String("tenantID", tenantID), elog.Int("count", len(defaultEnvs)))
	return nil
}
