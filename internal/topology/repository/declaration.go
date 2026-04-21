package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository/dao"
)

// DeclarationRepository 声明式注册数据仓储接口
type DeclarationRepository interface {
	// Upsert 插入或更新声明数据
	Upsert(ctx context.Context, decl domain.LinkDeclaration) error
	// FindBySource 按上报方标识查询声明数据
	FindBySource(ctx context.Context, tenantID, source string) ([]domain.LinkDeclaration, error)
	// FindAll 查询租户下所有声明数据
	FindAll(ctx context.Context, tenantID string) ([]domain.LinkDeclaration, error)
	// DeleteBySource 按上报方标识批量删除声明数据
	DeleteBySource(ctx context.Context, tenantID, source string) (int64, error)
	// InitIndexes 初始化索引
	InitIndexes(ctx context.Context) error
}

// declarationRepository DeclarationRepository 的 MongoDB 实现
type declarationRepository struct {
	dao *dao.DeclarationDAO
}

// NewDeclarationRepository 创建声明仓储
func NewDeclarationRepository(dao *dao.DeclarationDAO) DeclarationRepository {
	return &declarationRepository{dao: dao}
}

func (r *declarationRepository) Upsert(ctx context.Context, decl domain.LinkDeclaration) error {
	return r.dao.Upsert(ctx, decl)
}

func (r *declarationRepository) FindBySource(ctx context.Context, tenantID, source string) ([]domain.LinkDeclaration, error) {
	return r.dao.FindBySource(ctx, tenantID, source)
}

func (r *declarationRepository) FindAll(ctx context.Context, tenantID string) ([]domain.LinkDeclaration, error) {
	return r.dao.FindAll(ctx, tenantID)
}

func (r *declarationRepository) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	return r.dao.DeleteBySource(ctx, tenantID, source)
}

func (r *declarationRepository) InitIndexes(ctx context.Context) error {
	return r.dao.InitIndexes(ctx)
}
