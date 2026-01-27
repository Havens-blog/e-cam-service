package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const NodeCollection = "c_service_tree_node"

// Node 服务树节点 DAO 模型
type Node struct {
	ID          int64    `bson:"id"`
	UID         string   `bson:"uid"`
	Name        string   `bson:"name"`
	ParentID    int64    `bson:"parent_id"`
	Level       int      `bson:"level"`
	Path        string   `bson:"path"`
	TenantID    string   `bson:"tenant_id"`
	Owner       string   `bson:"owner"`
	Team        string   `bson:"team"`
	Description string   `bson:"description"`
	Tags        []string `bson:"tags"`
	Order       int      `bson:"order"`
	Status      int      `bson:"status"`
	Ctime       int64    `bson:"ctime"`
	Utime       int64    `bson:"utime"`
}

// NodeFilter DAO 层过滤条件
type NodeFilter struct {
	TenantID string
	ParentID *int64
	Level    int
	Status   *int
	Name     string
	Owner    string
	Team     string
	Offset   int64
	Limit    int64
}

// NodeDAO 服务树节点数据访问接口
type NodeDAO interface {
	Create(ctx context.Context, node Node) (int64, error)
	Update(ctx context.Context, node Node) error
	UpdatePath(ctx context.Context, id int64, path string) error
	GetByID(ctx context.Context, id int64) (Node, error)
	GetByUID(ctx context.Context, tenantID, uid string) (Node, error)
	List(ctx context.Context, filter NodeFilter) ([]Node, error)
	ListByPath(ctx context.Context, tenantID, pathPrefix string) ([]Node, error)
	Count(ctx context.Context, filter NodeFilter) (int64, error)
	CountChildren(ctx context.Context, parentID int64) (int64, error)
	Delete(ctx context.Context, id int64) error
}

type nodeDAO struct {
	db *mongox.Mongo
}

// NewNodeDAO 创建节点 DAO
func NewNodeDAO(db *mongox.Mongo) NodeDAO {
	return &nodeDAO{db: db}
}

func (d *nodeDAO) Create(ctx context.Context, node Node) (int64, error) {
	now := time.Now().UnixMilli()
	node.Ctime = now
	node.Utime = now

	if node.ID == 0 {
		node.ID = d.db.GetIdGenerator(NodeCollection)
	}

	_, err := d.db.Collection(NodeCollection).InsertOne(ctx, node)
	if err != nil {
		return 0, err
	}
	return node.ID, nil
}

func (d *nodeDAO) Update(ctx context.Context, node Node) error {
	node.Utime = time.Now().UnixMilli()

	filter := bson.M{"id": node.ID}
	update := bson.M{
		"$set": bson.M{
			"uid":         node.UID,
			"name":        node.Name,
			"parent_id":   node.ParentID,
			"level":       node.Level,
			"path":        node.Path,
			"owner":       node.Owner,
			"team":        node.Team,
			"description": node.Description,
			"tags":        node.Tags,
			"order":       node.Order,
			"status":      node.Status,
			"utime":       node.Utime,
		},
	}

	result, err := d.db.Collection(NodeCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *nodeDAO) UpdatePath(ctx context.Context, id int64, path string) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"path":  path,
			"utime": time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(NodeCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *nodeDAO) GetByID(ctx context.Context, id int64) (Node, error) {
	var node Node
	filter := bson.M{"id": id}
	err := d.db.Collection(NodeCollection).FindOne(ctx, filter).Decode(&node)
	return node, err
}

func (d *nodeDAO) GetByUID(ctx context.Context, tenantID, uid string) (Node, error) {
	var node Node
	filter := bson.M{"tenant_id": tenantID, "uid": uid}
	err := d.db.Collection(NodeCollection).FindOne(ctx, filter).Decode(&node)
	return node, err
}

func (d *nodeDAO) List(ctx context.Context, filter NodeFilter) ([]Node, error) {
	query := d.buildQuery(filter)

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "order", Value: 1}, {Key: "ctime", Value: 1}})

	cursor, err := d.db.Collection(NodeCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var nodes []Node
	err = cursor.All(ctx, &nodes)
	return nodes, err
}

// ListByPath 根据路径前缀查询子树
func (d *nodeDAO) ListByPath(ctx context.Context, tenantID, pathPrefix string) ([]Node, error) {
	filter := bson.M{
		"tenant_id": tenantID,
		"path":      bson.M{"$regex": "^" + pathPrefix},
	}

	opts := options.Find().SetSort(bson.D{{Key: "level", Value: 1}, {Key: "order", Value: 1}})

	cursor, err := d.db.Collection(NodeCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var nodes []Node
	err = cursor.All(ctx, &nodes)
	return nodes, err
}

func (d *nodeDAO) Count(ctx context.Context, filter NodeFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(NodeCollection).CountDocuments(ctx, query)
}

func (d *nodeDAO) CountChildren(ctx context.Context, parentID int64) (int64, error) {
	filter := bson.M{"parent_id": parentID}
	return d.db.Collection(NodeCollection).CountDocuments(ctx, filter)
}

func (d *nodeDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(NodeCollection).DeleteOne(ctx, filter)
	return err
}

func (d *nodeDAO) buildQuery(filter NodeFilter) bson.M {
	query := bson.M{}

	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.ParentID != nil {
		query["parent_id"] = *filter.ParentID
	}
	if filter.Level > 0 {
		query["level"] = filter.Level
	}
	if filter.Status != nil {
		query["status"] = *filter.Status
	}
	if filter.Name != "" {
		query["name"] = bson.M{"$regex": filter.Name, "$options": "i"}
	}
	if filter.Owner != "" {
		query["owner"] = filter.Owner
	}
	if filter.Team != "" {
		query["team"] = filter.Team
	}

	return query
}
