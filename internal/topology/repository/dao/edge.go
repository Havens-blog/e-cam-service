package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const TopoEdgesCollection = "topo_edges"

// EdgeDAO 拓扑连线 MongoDB 数据访问对象
type EdgeDAO struct {
	db *mongox.Mongo
}

// NewEdgeDAO 创建连线 DAO
func NewEdgeDAO(db *mongox.Mongo) *EdgeDAO {
	return &EdgeDAO{db: db}
}

func (d *EdgeDAO) col() *mongo.Collection {
	return d.db.Collection(TopoEdgesCollection)
}

// Upsert 插入或更新连线
func (d *EdgeDAO) Upsert(ctx context.Context, edge domain.TopoEdge) error {
	edge.UpdatedAt = time.Now()
	opts := options.Update().SetUpsert(true)
	_, err := d.col().UpdateOne(ctx, bson.M{"_id": edge.ID}, bson.M{"$set": edge}, opts)
	return err
}

// UpsertMany 批量插入或更新连线
func (d *EdgeDAO) UpsertMany(ctx context.Context, edges []domain.TopoEdge) error {
	if len(edges) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(edges))
	now := time.Now()
	for i := range edges {
		edges[i].UpdatedAt = now
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": edges[i].ID}).
			SetUpdate(bson.M{"$set": edges[i]}).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.col().BulkWrite(ctx, models, opts)
	return err
}

// Find 按过滤条件查询连线
func (d *EdgeDAO) Find(ctx context.Context, filter domain.EdgeFilter) ([]domain.TopoEdge, error) {
	query := d.buildEdgeQuery(filter)
	cursor, err := d.col().Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var edges []domain.TopoEdge
	if err = cursor.All(ctx, &edges); err != nil {
		return nil, err
	}
	return edges, nil
}

// FindBySourceID 查询指定源节点的所有出边
func (d *EdgeDAO) FindBySourceID(ctx context.Context, tenantID, sourceID string) ([]domain.TopoEdge, error) {
	cursor, err := d.col().Find(ctx, bson.M{"tenant_id": tenantID, "source_id": sourceID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var edges []domain.TopoEdge
	if err = cursor.All(ctx, &edges); err != nil {
		return nil, err
	}
	return edges, nil
}

// FindByTargetID 查询指定目标节点的所有入边
func (d *EdgeDAO) FindByTargetID(ctx context.Context, tenantID, targetID string) ([]domain.TopoEdge, error) {
	cursor, err := d.col().Find(ctx, bson.M{"tenant_id": tenantID, "target_id": targetID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var edges []domain.TopoEdge
	if err = cursor.All(ctx, &edges); err != nil {
		return nil, err
	}
	return edges, nil
}

// FindByNodeID 查询与指定节点相关的所有边（入边 + 出边）
func (d *EdgeDAO) FindByNodeID(ctx context.Context, tenantID, nodeID string) ([]domain.TopoEdge, error) {
	cursor, err := d.col().Find(ctx, bson.M{
		"tenant_id": tenantID,
		"$or": []bson.M{
			{"source_id": nodeID},
			{"target_id": nodeID},
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var edges []domain.TopoEdge
	if err = cursor.All(ctx, &edges); err != nil {
		return nil, err
	}
	return edges, nil
}

// Count 按过滤条件统计连线数量
func (d *EdgeDAO) Count(ctx context.Context, filter domain.EdgeFilter) (int64, error) {
	query := d.buildEdgeQuery(filter)
	return d.col().CountDocuments(ctx, query)
}

// Delete 删除连线
func (d *EdgeDAO) Delete(ctx context.Context, id string) error {
	_, err := d.col().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// DeleteBySource 按数据来源批量删除连线
func (d *EdgeDAO) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	result, err := d.col().DeleteMany(ctx, bson.M{
		"tenant_id":        tenantID,
		"source_collector": source,
	})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// DeleteByNodeID 删除与指定节点相关的所有边
func (d *EdgeDAO) DeleteByNodeID(ctx context.Context, tenantID, nodeID string) (int64, error) {
	result, err := d.col().DeleteMany(ctx, bson.M{
		"tenant_id": tenantID,
		"$or": []bson.M{
			{"source_id": nodeID},
			{"target_id": nodeID},
		},
	})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// UpdatePendingEdges 将目标节点匹配的 pending 边激活为 active
func (d *EdgeDAO) UpdatePendingEdges(ctx context.Context, tenantID, targetID string) (int64, error) {
	result, err := d.col().UpdateMany(ctx,
		bson.M{"tenant_id": tenantID, "target_id": targetID, "status": domain.EdgeStatusPending},
		bson.M{"$set": bson.M{"status": domain.EdgeStatusActive, "updated_at": time.Now()}},
	)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

// CountPending 统计 pending 状态的边数量
func (d *EdgeDAO) CountPending(ctx context.Context, tenantID string) (int64, error) {
	return d.col().CountDocuments(ctx, bson.M{"tenant_id": tenantID, "status": domain.EdgeStatusPending})
}

func (d *EdgeDAO) buildEdgeQuery(filter domain.EdgeFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if len(filter.SourceIDs) > 0 {
		query["source_id"] = bson.M{"$in": filter.SourceIDs}
	}
	if len(filter.TargetIDs) > 0 {
		query["target_id"] = bson.M{"$in": filter.TargetIDs}
	}
	if len(filter.Relations) > 0 {
		query["relation"] = bson.M{"$in": filter.Relations}
	}
	if len(filter.SourceCollectors) > 0 {
		query["source_collector"] = bson.M{"$in": filter.SourceCollectors}
	}
	if len(filter.Statuses) > 0 {
		query["status"] = bson.M{"$in": filter.Statuses}
	}
	if filter.HideSilent {
		threshold := time.Now().Add(-24 * time.Hour)
		query["$or"] = []bson.M{
			{"last_seen_at": nil},                       // 非日志来源
			{"last_seen_at": bson.M{"$gte": threshold}}, // 24h 内有流量
		}
	}
	return query
}

// InitIndexes 初始化连线集合索引
func (d *EdgeDAO) InitIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "source_id", Value: 1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "target_id", Value: 1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "last_seen_at", Value: 1}}},
	}
	_, err := d.col().Indexes().CreateMany(ctx, indexes)
	return err
}
