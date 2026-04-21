package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository/dao"
)

// EdgeRepository 拓扑连线仓储接口
type EdgeRepository interface {
	// Upsert 插入或更新单条连线
	Upsert(ctx context.Context, edge domain.TopoEdge) error
	// UpsertMany 批量插入或更新连线
	UpsertMany(ctx context.Context, edges []domain.TopoEdge) error
	// Find 按过滤条件查询连线
	Find(ctx context.Context, filter domain.EdgeFilter) ([]domain.TopoEdge, error)
	// FindBySourceID 查询指定源节点的所有出边
	FindBySourceID(ctx context.Context, tenantID, sourceID string) ([]domain.TopoEdge, error)
	// FindByTargetID 查询指定目标节点的所有入边
	FindByTargetID(ctx context.Context, tenantID, targetID string) ([]domain.TopoEdge, error)
	// FindByNodeID 查询与指定节点相关的所有边
	FindByNodeID(ctx context.Context, tenantID, nodeID string) ([]domain.TopoEdge, error)
	// Count 按过滤条件统计连线数量
	Count(ctx context.Context, filter domain.EdgeFilter) (int64, error)
	// Delete 删除连线
	Delete(ctx context.Context, id string) error
	// DeleteBySource 按数据来源批量删除连线
	DeleteBySource(ctx context.Context, tenantID, source string) (int64, error)
	// DeleteByNodeID 删除与指定节点相关的所有边
	DeleteByNodeID(ctx context.Context, tenantID, nodeID string) (int64, error)
	// UpdatePendingEdges 将目标节点匹配的 pending 边激活
	UpdatePendingEdges(ctx context.Context, tenantID, targetID string) (int64, error)
	// CountPending 统计 pending 状态的边数量
	CountPending(ctx context.Context, tenantID string) (int64, error)
	// InitIndexes 初始化索引
	InitIndexes(ctx context.Context) error
}

// edgeRepository EdgeRepository 的 MongoDB 实现
type edgeRepository struct {
	dao *dao.EdgeDAO
}

// NewEdgeRepository 创建连线仓储
func NewEdgeRepository(dao *dao.EdgeDAO) EdgeRepository {
	return &edgeRepository{dao: dao}
}

func (r *edgeRepository) Upsert(ctx context.Context, edge domain.TopoEdge) error {
	return r.dao.Upsert(ctx, edge)
}

func (r *edgeRepository) UpsertMany(ctx context.Context, edges []domain.TopoEdge) error {
	return r.dao.UpsertMany(ctx, edges)
}

func (r *edgeRepository) Find(ctx context.Context, filter domain.EdgeFilter) ([]domain.TopoEdge, error) {
	return r.dao.Find(ctx, filter)
}

func (r *edgeRepository) FindBySourceID(ctx context.Context, tenantID, sourceID string) ([]domain.TopoEdge, error) {
	return r.dao.FindBySourceID(ctx, tenantID, sourceID)
}

func (r *edgeRepository) FindByTargetID(ctx context.Context, tenantID, targetID string) ([]domain.TopoEdge, error) {
	return r.dao.FindByTargetID(ctx, tenantID, targetID)
}

func (r *edgeRepository) FindByNodeID(ctx context.Context, tenantID, nodeID string) ([]domain.TopoEdge, error) {
	return r.dao.FindByNodeID(ctx, tenantID, nodeID)
}

func (r *edgeRepository) Count(ctx context.Context, filter domain.EdgeFilter) (int64, error) {
	return r.dao.Count(ctx, filter)
}

func (r *edgeRepository) Delete(ctx context.Context, id string) error {
	return r.dao.Delete(ctx, id)
}

func (r *edgeRepository) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	return r.dao.DeleteBySource(ctx, tenantID, source)
}

func (r *edgeRepository) DeleteByNodeID(ctx context.Context, tenantID, nodeID string) (int64, error) {
	return r.dao.DeleteByNodeID(ctx, tenantID, nodeID)
}

func (r *edgeRepository) UpdatePendingEdges(ctx context.Context, tenantID, targetID string) (int64, error) {
	return r.dao.UpdatePendingEdges(ctx, tenantID, targetID)
}

func (r *edgeRepository) CountPending(ctx context.Context, tenantID string) (int64, error) {
	return r.dao.CountPending(ctx, tenantID)
}

func (r *edgeRepository) InitIndexes(ctx context.Context) error {
	return r.dao.InitIndexes(ctx)
}
