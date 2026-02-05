package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const RuleCollection = "c_binding_rule"

// RuleCondition 规则条件
type RuleCondition struct {
	Field    string `bson:"field"`
	Operator string `bson:"operator"`
	Value    string `bson:"value"`
}

// Rule 绑定规则 DAO 模型
type Rule struct {
	ID          int64           `bson:"id"`
	NodeID      int64           `bson:"node_id"`
	EnvID       int64           `bson:"env_id"`
	Name        string          `bson:"name"`
	TenantID    string          `bson:"tenant_id"`
	Priority    int             `bson:"priority"`
	Conditions  []RuleCondition `bson:"conditions"`
	Enabled     bool            `bson:"enabled"`
	Description string          `bson:"description"`
	Ctime       int64           `bson:"ctime"`
	Utime       int64           `bson:"utime"`
}

// RuleFilter DAO 层过滤条件
type RuleFilter struct {
	TenantID string
	NodeID   int64
	Enabled  *bool
	Name     string
	Offset   int64
	Limit    int64
}

// RuleDAO 绑定规则数据访问接口
type RuleDAO interface {
	Create(ctx context.Context, rule Rule) (int64, error)
	Update(ctx context.Context, rule Rule) error
	GetByID(ctx context.Context, id int64) (Rule, error)
	List(ctx context.Context, filter RuleFilter) ([]Rule, error)
	ListEnabled(ctx context.Context, tenantID string) ([]Rule, error)
	Count(ctx context.Context, filter RuleFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	DeleteByNodeID(ctx context.Context, nodeID int64) error
}

type ruleDAO struct {
	db *mongox.Mongo
}

// NewRuleDAO 创建规则 DAO
func NewRuleDAO(db *mongox.Mongo) RuleDAO {
	return &ruleDAO{db: db}
}

func (d *ruleDAO) Create(ctx context.Context, rule Rule) (int64, error) {
	now := time.Now().UnixMilli()
	rule.Ctime = now
	rule.Utime = now

	if rule.ID == 0 {
		rule.ID = d.db.GetIdGenerator(RuleCollection)
	}

	_, err := d.db.Collection(RuleCollection).InsertOne(ctx, rule)
	if err != nil {
		return 0, err
	}
	return rule.ID, nil
}

func (d *ruleDAO) Update(ctx context.Context, rule Rule) error {
	rule.Utime = time.Now().UnixMilli()

	filter := bson.M{"id": rule.ID}
	update := bson.M{
		"$set": bson.M{
			"node_id":     rule.NodeID,
			"env_id":      rule.EnvID,
			"name":        rule.Name,
			"priority":    rule.Priority,
			"conditions":  rule.Conditions,
			"enabled":     rule.Enabled,
			"description": rule.Description,
			"utime":       rule.Utime,
		},
	}

	result, err := d.db.Collection(RuleCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *ruleDAO) GetByID(ctx context.Context, id int64) (Rule, error) {
	var rule Rule
	filter := bson.M{"id": id}
	err := d.db.Collection(RuleCollection).FindOne(ctx, filter).Decode(&rule)
	return rule, err
}

func (d *ruleDAO) List(ctx context.Context, filter RuleFilter) ([]Rule, error) {
	query := d.buildQuery(filter)

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "priority", Value: 1}, {Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(RuleCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []Rule
	err = cursor.All(ctx, &rules)
	return rules, err
}

// ListEnabled 获取所有启用的规则，按优先级排序
func (d *ruleDAO) ListEnabled(ctx context.Context, tenantID string) ([]Rule, error) {
	filter := bson.M{
		"tenant_id": tenantID,
		"enabled":   true,
	}

	opts := options.Find().SetSort(bson.D{{Key: "priority", Value: 1}})

	cursor, err := d.db.Collection(RuleCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []Rule
	err = cursor.All(ctx, &rules)
	return rules, err
}

func (d *ruleDAO) Count(ctx context.Context, filter RuleFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(RuleCollection).CountDocuments(ctx, query)
}

func (d *ruleDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(RuleCollection).DeleteOne(ctx, filter)
	return err
}

func (d *ruleDAO) DeleteByNodeID(ctx context.Context, nodeID int64) error {
	filter := bson.M{"node_id": nodeID}
	_, err := d.db.Collection(RuleCollection).DeleteMany(ctx, filter)
	return err
}

func (d *ruleDAO) buildQuery(filter RuleFilter) bson.M {
	query := bson.M{}

	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.NodeID > 0 {
		query["node_id"] = filter.NodeID
	}
	if filter.Enabled != nil {
		query["enabled"] = *filter.Enabled
	}
	if filter.Name != "" {
		query["name"] = bson.M{"$regex": filter.Name, "$options": "i"}
	}

	return query
}
