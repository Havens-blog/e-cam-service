package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const BindingCollection = "c_resource_binding"

// Binding 资源绑定 DAO 模型
type Binding struct {
	ID           int64  `bson:"id"`
	NodeID       int64  `bson:"node_id"`
	EnvID        int64  `bson:"env_id"`
	ResourceType string `bson:"resource_type"`
	ResourceID   int64  `bson:"resource_id"`
	TenantID     string `bson:"tenant_id"`
	BindType     string `bson:"bind_type"`
	RuleID       int64  `bson:"rule_id"`
	Ctime        int64  `bson:"ctime"`
}

// BindingFilter DAO 层过滤条件
type BindingFilter struct {
	TenantID     string
	NodeID       int64
	EnvID        int64
	ResourceType string
	ResourceID   int64
	BindType     string
	Offset       int64
	Limit        int64
}

// BindingDAO 资源绑定数据访问接口
type BindingDAO interface {
	Create(ctx context.Context, binding Binding) (int64, error)
	CreateBatch(ctx context.Context, bindings []Binding) (int64, error)
	GetByID(ctx context.Context, id int64) (Binding, error)
	GetByResource(ctx context.Context, tenantID, resourceType string, resourceID int64) (Binding, error)
	List(ctx context.Context, filter BindingFilter) ([]Binding, error)
	Count(ctx context.Context, filter BindingFilter) (int64, error)
	CountByNodeID(ctx context.Context, nodeID int64) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByNodeID(ctx context.Context, nodeID int64) error
	DeleteByResource(ctx context.Context, tenantID, resourceType string, resourceID int64) error
	DeleteByRuleID(ctx context.Context, ruleID int64) error
}

type bindingDAO struct {
	db *mongox.Mongo
}

// NewBindingDAO 创建绑定 DAO
func NewBindingDAO(db *mongox.Mongo) BindingDAO {
	return &bindingDAO{db: db}
}

func (d *bindingDAO) Create(ctx context.Context, binding Binding) (int64, error) {
	binding.Ctime = time.Now().UnixMilli()

	if binding.ID == 0 {
		binding.ID = d.db.GetIdGenerator(BindingCollection)
	}

	_, err := d.db.Collection(BindingCollection).InsertOne(ctx, binding)
	if err != nil {
		return 0, err
	}
	return binding.ID, nil
}

func (d *bindingDAO) CreateBatch(ctx context.Context, bindings []Binding) (int64, error) {
	if len(bindings) == 0 {
		return 0, nil
	}

	now := time.Now().UnixMilli()
	docs := make([]any, len(bindings))

	for i := range bindings {
		if bindings[i].ID == 0 {
			bindings[i].ID = d.db.GetIdGenerator(BindingCollection)
		}
		bindings[i].Ctime = now
		docs[i] = bindings[i]
	}

	result, err := d.db.Collection(BindingCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *bindingDAO) GetByID(ctx context.Context, id int64) (Binding, error) {
	var binding Binding
	filter := bson.M{"id": id}
	err := d.db.Collection(BindingCollection).FindOne(ctx, filter).Decode(&binding)
	return binding, err
}

func (d *bindingDAO) GetByResource(ctx context.Context, tenantID, resourceType string, resourceID int64) (Binding, error) {
	var binding Binding
	filter := bson.M{
		"tenant_id":     tenantID,
		"resource_type": resourceType,
		"resource_id":   resourceID,
	}
	err := d.db.Collection(BindingCollection).FindOne(ctx, filter).Decode(&binding)
	return binding, err
}

func (d *bindingDAO) List(ctx context.Context, filter BindingFilter) ([]Binding, error) {
	query := d.buildQuery(filter)

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := d.db.Collection(BindingCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var bindings []Binding
	err = cursor.All(ctx, &bindings)
	return bindings, err
}

func (d *bindingDAO) Count(ctx context.Context, filter BindingFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(BindingCollection).CountDocuments(ctx, query)
}

func (d *bindingDAO) CountByNodeID(ctx context.Context, nodeID int64) (int64, error) {
	filter := bson.M{"node_id": nodeID}
	return d.db.Collection(BindingCollection).CountDocuments(ctx, filter)
}

func (d *bindingDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(BindingCollection).DeleteOne(ctx, filter)
	return err
}

func (d *bindingDAO) DeleteByNodeID(ctx context.Context, nodeID int64) error {
	filter := bson.M{"node_id": nodeID}
	_, err := d.db.Collection(BindingCollection).DeleteMany(ctx, filter)
	return err
}

func (d *bindingDAO) DeleteByResource(ctx context.Context, tenantID, resourceType string, resourceID int64) error {
	filter := bson.M{
		"tenant_id":     tenantID,
		"resource_type": resourceType,
		"resource_id":   resourceID,
	}
	_, err := d.db.Collection(BindingCollection).DeleteOne(ctx, filter)
	return err
}

func (d *bindingDAO) DeleteByRuleID(ctx context.Context, ruleID int64) error {
	filter := bson.M{"rule_id": ruleID}
	_, err := d.db.Collection(BindingCollection).DeleteMany(ctx, filter)
	return err
}

func (d *bindingDAO) buildQuery(filter BindingFilter) bson.M {
	query := bson.M{}

	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.NodeID > 0 {
		query["node_id"] = filter.NodeID
	}
	if filter.EnvID > 0 {
		query["env_id"] = filter.EnvID
	}
	if filter.ResourceType != "" {
		query["resource_type"] = filter.ResourceType
	}
	if filter.ResourceID > 0 {
		query["resource_id"] = filter.ResourceID
	}
	if filter.BindType != "" {
		query["bind_type"] = filter.BindType
	}

	return query
}
