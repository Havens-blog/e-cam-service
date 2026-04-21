package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository/dao"
)

// NodeRepository 拓扑节点仓储接口
type NodeRepository interface {
	// Upsert 插入或更新单个节点
	Upsert(ctx context.Context, node domain.TopoNode) error
	// UpsertMany 批量插入或更新节点
	UpsertMany(ctx context.Context, nodes []domain.TopoNode) error
	// FindByID 根据 ID 查询节点
	FindByID(ctx context.Context, id string) (domain.TopoNode, error)
	// FindByIDs 根据 ID 列表批量查询节点
	FindByIDs(ctx context.Context, ids []string) ([]domain.TopoNode, error)
	// Find 按过滤条件查询节点
	Find(ctx context.Context, filter domain.NodeFilter) ([]domain.TopoNode, error)
	// Count 按过滤条件统计节点数量
	Count(ctx context.Context, filter domain.NodeFilter) (int64, error)
	// Delete 删除节点
	Delete(ctx context.Context, id string) error
	// DeleteBySource 按数据来源批量删除节点
	DeleteBySource(ctx context.Context, tenantID, source string) (int64, error)
	// FindDNSEntries 查询所有 DNS 入口节点
	FindDNSEntries(ctx context.Context, tenantID string) ([]domain.TopoNode, error)
	// InitIndexes 初始化索引
	InitIndexes(ctx context.Context) error
}

// nodeRepository NodeRepository 的 MongoDB 实现
type nodeRepository struct {
	dao *dao.NodeDAO
}

// NewNodeRepository 创建节点仓储
func NewNodeRepository(dao *dao.NodeDAO) NodeRepository {
	return &nodeRepository{dao: dao}
}

func (r *nodeRepository) Upsert(ctx context.Context, node domain.TopoNode) error {
	return r.dao.Upsert(ctx, node)
}

func (r *nodeRepository) UpsertMany(ctx context.Context, nodes []domain.TopoNode) error {
	return r.dao.UpsertMany(ctx, nodes)
}

func (r *nodeRepository) FindByID(ctx context.Context, id string) (domain.TopoNode, error) {
	return r.dao.FindByID(ctx, id)
}

func (r *nodeRepository) FindByIDs(ctx context.Context, ids []string) ([]domain.TopoNode, error) {
	return r.dao.FindByIDs(ctx, ids)
}

func (r *nodeRepository) Find(ctx context.Context, filter domain.NodeFilter) ([]domain.TopoNode, error) {
	return r.dao.Find(ctx, filter)
}

func (r *nodeRepository) Count(ctx context.Context, filter domain.NodeFilter) (int64, error) {
	return r.dao.Count(ctx, filter)
}

func (r *nodeRepository) Delete(ctx context.Context, id string) error {
	return r.dao.Delete(ctx, id)
}

func (r *nodeRepository) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	return r.dao.DeleteBySource(ctx, tenantID, source)
}

func (r *nodeRepository) FindDNSEntries(ctx context.Context, tenantID string) ([]domain.TopoNode, error) {
	return r.dao.FindDNSEntries(ctx, tenantID)
}

func (r *nodeRepository) InitIndexes(ctx context.Context) error {
	return r.dao.InitIndexes(ctx)
}
