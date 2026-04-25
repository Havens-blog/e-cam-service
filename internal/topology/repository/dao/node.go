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

const TopoNodesCollection = "ecam_topo_node"

// NodeDAO 拓扑节点 MongoDB 数据访问对象
type NodeDAO struct {
	db *mongox.Mongo
}

// NewNodeDAO 创建节点 DAO
func NewNodeDAO(db *mongox.Mongo) *NodeDAO {
	return &NodeDAO{db: db}
}

func (d *NodeDAO) col() *mongo.Collection {
	return d.db.Collection(TopoNodesCollection)
}

// Upsert 插入或更新节点
func (d *NodeDAO) Upsert(ctx context.Context, node domain.TopoNode) error {
	node.UpdatedAt = time.Now()
	opts := options.Update().SetUpsert(true)
	_, err := d.col().UpdateOne(ctx, bson.M{"_id": node.ID}, bson.M{"$set": node}, opts)
	return err
}

// UpsertMany 批量插入或更新节点
func (d *NodeDAO) UpsertMany(ctx context.Context, nodes []domain.TopoNode) error {
	if len(nodes) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(nodes))
	now := time.Now()
	for i := range nodes {
		nodes[i].UpdatedAt = now
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": nodes[i].ID}).
			SetUpdate(bson.M{"$set": nodes[i]}).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.col().BulkWrite(ctx, models, opts)
	return err
}

// FindByID 根据 ID 查询节点
func (d *NodeDAO) FindByID(ctx context.Context, id string) (domain.TopoNode, error) {
	var node domain.TopoNode
	err := d.col().FindOne(ctx, bson.M{"_id": id}).Decode(&node)
	if err == mongo.ErrNoDocuments {
		return domain.TopoNode{}, nil
	}
	return node, err
}

// FindByIDs 根据 ID 列表批量查询节点
func (d *NodeDAO) FindByIDs(ctx context.Context, ids []string) ([]domain.TopoNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	cursor, err := d.col().Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var nodes []domain.TopoNode
	if err = cursor.All(ctx, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// Find 按过滤条件查询节点
func (d *NodeDAO) Find(ctx context.Context, filter domain.NodeFilter) ([]domain.TopoNode, error) {
	query := d.buildNodeQuery(filter)
	cursor, err := d.col().Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var nodes []domain.TopoNode
	if err = cursor.All(ctx, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// Count 按过滤条件统计节点数量
func (d *NodeDAO) Count(ctx context.Context, filter domain.NodeFilter) (int64, error) {
	query := d.buildNodeQuery(filter)
	return d.col().CountDocuments(ctx, query)
}

// Delete 删除节点
func (d *NodeDAO) Delete(ctx context.Context, id string) error {
	_, err := d.col().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// DeleteBySource 按数据来源批量删除节点
func (d *NodeDAO) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	result, err := d.col().DeleteMany(ctx, bson.M{
		"tenant_id":        tenantID,
		"source_collector": source,
	})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// FindDNSEntries 查询所有 DNS 入口节点
func (d *NodeDAO) FindDNSEntries(ctx context.Context, tenantID string) ([]domain.TopoNode, error) {
	cursor, err := d.col().Find(ctx, bson.M{
		"tenant_id": tenantID,
		"type":      domain.NodeTypeDNSRecord,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var nodes []domain.TopoNode
	if err = cursor.All(ctx, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (d *NodeDAO) buildNodeQuery(filter domain.NodeFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if len(filter.Types) > 0 {
		query["type"] = bson.M{"$in": filter.Types}
	}
	if len(filter.Categories) > 0 {
		query["category"] = bson.M{"$in": filter.Categories}
	}
	if len(filter.Providers) > 0 {
		query["provider"] = bson.M{"$in": filter.Providers}
	}
	if len(filter.Regions) > 0 {
		query["region"] = bson.M{"$in": filter.Regions}
	}
	if len(filter.SourceCollectors) > 0 {
		query["source_collector"] = bson.M{"$in": filter.SourceCollectors}
	}
	if len(filter.IDs) > 0 {
		query["_id"] = bson.M{"$in": filter.IDs}
	}
	return query
}

// InitIndexes 初始化节点集合索引
func (d *NodeDAO) InitIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "type", Value: 1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "provider", Value: 1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "category", Value: 1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "source_collector", Value: 1}}},
	}
	_, err := d.col().Indexes().CreateMany(ctx, indexes)
	return err
}
