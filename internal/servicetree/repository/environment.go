package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// EnvironmentRepository 环境仓储接口
type EnvironmentRepository interface {
	Create(ctx context.Context, env domain.Environment) (int64, error)
	CreateBatch(ctx context.Context, envs []domain.Environment) (int64, error)
	Update(ctx context.Context, env domain.Environment) error
	GetByID(ctx context.Context, id int64) (domain.Environment, error)
	GetByCode(ctx context.Context, tenantID, code string) (domain.Environment, error)
	List(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error)
	Count(ctx context.Context, filter domain.EnvironmentFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
}

type environmentRepository struct {
	dao dao.EnvironmentDAO
}

// NewEnvironmentRepository 创建环境仓储
func NewEnvironmentRepository(dao dao.EnvironmentDAO) EnvironmentRepository {
	return &environmentRepository{dao: dao}
}

func (r *environmentRepository) Create(ctx context.Context, env domain.Environment) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(env))
}

func (r *environmentRepository) CreateBatch(ctx context.Context, envs []domain.Environment) (int64, error) {
	daoEnvs := make([]dao.Environment, len(envs))
	for i, e := range envs {
		daoEnvs[i] = r.toDAO(e)
	}
	return r.dao.CreateBatch(ctx, daoEnvs)
}

func (r *environmentRepository) Update(ctx context.Context, env domain.Environment) error {
	return r.dao.Update(ctx, r.toDAO(env))
}

func (r *environmentRepository) GetByID(ctx context.Context, id int64) (domain.Environment, error) {
	daoEnv, err := r.dao.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.Environment{}, domain.ErrEnvNotFound
		}
		return domain.Environment{}, err
	}
	return r.toDomain(daoEnv), nil
}

func (r *environmentRepository) GetByCode(ctx context.Context, tenantID, code string) (domain.Environment, error) {
	daoEnv, err := r.dao.GetByCode(ctx, tenantID, code)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.Environment{}, domain.ErrEnvNotFound
		}
		return domain.Environment{}, err
	}
	return r.toDomain(daoEnv), nil
}

func (r *environmentRepository) List(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
	daoEnvs, err := r.dao.List(ctx, r.toDAOFilter(filter))
	if err != nil {
		return nil, err
	}

	envs := make([]domain.Environment, len(daoEnvs))
	for i, e := range daoEnvs {
		envs[i] = r.toDomain(e)
	}
	return envs, nil
}

func (r *environmentRepository) Count(ctx context.Context, filter domain.EnvironmentFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

func (r *environmentRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

func (r *environmentRepository) toDAO(env domain.Environment) dao.Environment {
	return dao.Environment{
		ID:          env.ID,
		Code:        env.Code,
		Name:        env.Name,
		TenantID:    env.TenantID,
		Description: env.Description,
		Color:       env.Color,
		Order:       env.Order,
		Status:      env.Status,
	}
}

func (r *environmentRepository) toDomain(daoEnv dao.Environment) domain.Environment {
	return domain.Environment{
		ID:          daoEnv.ID,
		Code:        daoEnv.Code,
		Name:        daoEnv.Name,
		TenantID:    daoEnv.TenantID,
		Description: daoEnv.Description,
		Color:       daoEnv.Color,
		Order:       daoEnv.Order,
		Status:      daoEnv.Status,
		CreateTime:  time.UnixMilli(daoEnv.Ctime),
		UpdateTime:  time.UnixMilli(daoEnv.Utime),
	}
}

func (r *environmentRepository) toDAOFilter(filter domain.EnvironmentFilter) dao.EnvironmentFilter {
	return dao.EnvironmentFilter{
		TenantID: filter.TenantID,
		Code:     filter.Code,
		Status:   filter.Status,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}
}
