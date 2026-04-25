package tag

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const TagPolicyCollection = "ecam_tag_policy"
const TagRuleCollection = "ecam_tag_rule"

// TagDAO 标签策略与规则数据访问接口
type TagDAO interface {
	InsertPolicy(ctx context.Context, policy TagPolicy) (int64, error)
	UpdatePolicy(ctx context.Context, policy TagPolicy) error
	DeletePolicy(ctx context.Context, id int64) error
	GetPolicyByID(ctx context.Context, id int64) (TagPolicy, error)
	ListPolicies(ctx context.Context, filter PolicyFilter) ([]TagPolicy, int64, error)

	InsertRule(ctx context.Context, rule TagRule) (int64, error)
	UpdateRule(ctx context.Context, rule TagRule) error
	DeleteRule(ctx context.Context, id int64) error
	GetRuleByID(ctx context.Context, id int64) (TagRule, error)
	ListRules(ctx context.Context, filter RuleFilter) ([]TagRule, int64, error)
	ListEnabledRules(ctx context.Context, tenantID string) ([]TagRule, error)
}

type tagDAO struct {
	db *mongox.Mongo
}

// NewTagDAO 创建标签策略 DAO
func NewTagDAO(db *mongox.Mongo) TagDAO {
	return &tagDAO{db: db}
}

func (d *tagDAO) InsertPolicy(ctx context.Context, policy TagPolicy) (int64, error) {
	now := time.Now().UnixMilli()
	policy.Ctime = now
	policy.Utime = now
	if policy.ID == 0 {
		policy.ID = d.db.GetIdGenerator(TagPolicyCollection)
	}
	_, err := d.db.Collection(TagPolicyCollection).InsertOne(ctx, policy)
	if err != nil {
		return 0, err
	}
	return policy.ID, nil
}

func (d *tagDAO) UpdatePolicy(ctx context.Context, policy TagPolicy) error {
	filter := bson.M{"id": policy.ID, "tenant_id": policy.TenantID}
	update := bson.M{
		"$set": bson.M{
			"name":                  policy.Name,
			"description":           policy.Description,
			"required_keys":         policy.RequiredKeys,
			"key_value_constraints": policy.KeyValueConstraints,
			"resource_types":        policy.ResourceTypes,
			"status":                policy.Status,
			"utime":                 time.Now().UnixMilli(),
		},
	}
	result, err := d.db.Collection(TagPolicyCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *tagDAO) DeletePolicy(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(TagPolicyCollection).DeleteOne(ctx, filter)
	return err
}

func (d *tagDAO) GetPolicyByID(ctx context.Context, id int64) (TagPolicy, error) {
	var policy TagPolicy
	filter := bson.M{"id": id}
	err := d.db.Collection(TagPolicyCollection).FindOne(ctx, filter).Decode(&policy)
	return policy, err
}

func (d *tagDAO) ListPolicies(ctx context.Context, filter PolicyFilter) ([]TagPolicy, int64, error) {
	query := bson.M{"tenant_id": filter.TenantID}

	total, err := d.db.Collection(TagPolicyCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(TagPolicyCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var policies []TagPolicy
	if err = cursor.All(ctx, &policies); err != nil {
		return nil, 0, err
	}
	return policies, total, nil
}

// ==================== TagRule CRUD ====================

func (d *tagDAO) InsertRule(ctx context.Context, rule TagRule) (int64, error) {
	now := time.Now().UnixMilli()
	rule.Ctime = now
	rule.Utime = now
	if rule.ID == 0 {
		rule.ID = d.db.GetIdGenerator(TagRuleCollection)
	}
	_, err := d.db.Collection(TagRuleCollection).InsertOne(ctx, rule)
	if err != nil {
		return 0, err
	}
	return rule.ID, nil
}

func (d *tagDAO) UpdateRule(ctx context.Context, rule TagRule) error {
	filter := bson.M{"id": rule.ID, "tenant_id": rule.TenantID}
	update := bson.M{
		"$set": bson.M{
			"name":        rule.Name,
			"description": rule.Description,
			"logic":       rule.Logic,
			"conditions":  rule.Conditions,
			"tags":        rule.Tags,
			"priority":    rule.Priority,
			"status":      rule.Status,
			"utime":       time.Now().UnixMilli(),
		},
	}
	result, err := d.db.Collection(TagRuleCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *tagDAO) DeleteRule(ctx context.Context, id int64) error {
	_, err := d.db.Collection(TagRuleCollection).DeleteOne(ctx, bson.M{"id": id})
	return err
}

func (d *tagDAO) GetRuleByID(ctx context.Context, id int64) (TagRule, error) {
	var rule TagRule
	err := d.db.Collection(TagRuleCollection).FindOne(ctx, bson.M{"id": id}).Decode(&rule)
	return rule, err
}

func (d *tagDAO) ListRules(ctx context.Context, filter RuleFilter) ([]TagRule, int64, error) {
	query := bson.M{"tenant_id": filter.TenantID}
	total, err := d.db.Collection(TagRuleCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "priority", Value: 1}, {Key: "ctime", Value: -1}})
	cursor, err := d.db.Collection(TagRuleCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var rules []TagRule
	if err = cursor.All(ctx, &rules); err != nil {
		return nil, 0, err
	}
	return rules, total, nil
}

func (d *tagDAO) ListEnabledRules(ctx context.Context, tenantID string) ([]TagRule, error) {
	query := bson.M{"tenant_id": tenantID, "status": "enabled"}
	opts := options.Find().SetSort(bson.D{{Key: "priority", Value: 1}})
	cursor, err := d.db.Collection(TagRuleCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rules []TagRule
	if err = cursor.All(ctx, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}
