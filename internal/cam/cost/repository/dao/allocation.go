package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	AllocationCollection     = "cost_allocations"
	AllocationRuleCollection = "cost_allocation_rules"
	DefaultPolicyCollection  = "cost_allocation_default_policy"
)

type allocationDAO struct {
	db *mongox.Mongo
}

// NewAllocationDAO 创建成本分摊 DAO
func NewAllocationDAO(db *mongox.Mongo) repository.AllocationDAO {
	return &allocationDAO{db: db}
}

// --- 分摊规则 ---

func (d *allocationDAO) CreateRule(ctx context.Context, rule domain.AllocationRule) (int64, error) {
	now := time.Now().UnixMilli()
	rule.CreateTime = now
	rule.UpdateTime = now
	if rule.ID == 0 {
		rule.ID = d.db.GetIdGenerator(AllocationRuleCollection)
	}
	_, err := d.db.Collection(AllocationRuleCollection).InsertOne(ctx, rule)
	if err != nil {
		return 0, err
	}
	return rule.ID, nil
}

func (d *allocationDAO) UpdateRule(ctx context.Context, rule domain.AllocationRule) error {
	rule.UpdateTime = time.Now().UnixMilli()
	filter := bson.M{"id": rule.ID}
	update := bson.M{"$set": bson.M{
		"name":             rule.Name,
		"rule_type":        rule.RuleType,
		"dimension_combos": rule.DimensionCombos,
		"tag_key":          rule.TagKey,
		"tag_value_map":    rule.TagValueMap,
		"shared_config":    rule.SharedConfig,
		"priority":         rule.Priority,
		"status":           rule.Status,
		"utime":            rule.UpdateTime,
	}}
	result, err := d.db.Collection(AllocationRuleCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *allocationDAO) GetRuleByID(ctx context.Context, id int64) (domain.AllocationRule, error) {
	var rule domain.AllocationRule
	filter := bson.M{"id": id}
	err := d.db.Collection(AllocationRuleCollection).FindOne(ctx, filter).Decode(&rule)
	return rule, err
}

func (d *allocationDAO) ListRules(ctx context.Context, filter repository.AllocationRuleFilter) ([]domain.AllocationRule, error) {
	query := d.buildRuleQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "priority", Value: 1}, {Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(AllocationRuleCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []domain.AllocationRule
	err = cursor.All(ctx, &rules)
	return rules, err
}

func (d *allocationDAO) ListActiveRules(ctx context.Context, tenantID string) ([]domain.AllocationRule, error) {
	filter := bson.M{
		"tenant_id": tenantID,
		"status":    "active",
	}
	opts := options.Find().SetSort(bson.D{{Key: "priority", Value: 1}})

	cursor, err := d.db.Collection(AllocationRuleCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []domain.AllocationRule
	err = cursor.All(ctx, &rules)
	return rules, err
}

func (d *allocationDAO) DeleteRule(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(AllocationRuleCollection).DeleteOne(ctx, filter)
	return err
}

// --- 默认分摊策略 ---

func (d *allocationDAO) SaveDefaultPolicy(ctx context.Context, policy domain.DefaultAllocationPolicy) error {
	now := time.Now().UnixMilli()
	policy.UpdateTime = now
	if policy.ID == 0 {
		policy.ID = d.db.GetIdGenerator(DefaultPolicyCollection)
	}
	if policy.CreateTime == 0 {
		policy.CreateTime = now
	}

	filter := bson.M{"tenant_id": policy.TenantID}
	update := bson.M{"$set": policy}
	opts := options.Update().SetUpsert(true)
	_, err := d.db.Collection(DefaultPolicyCollection).UpdateOne(ctx, filter, update, opts)
	return err
}

func (d *allocationDAO) GetDefaultPolicy(ctx context.Context, tenantID string) (domain.DefaultAllocationPolicy, error) {
	var policy domain.DefaultAllocationPolicy
	filter := bson.M{"tenant_id": tenantID}
	err := d.db.Collection(DefaultPolicyCollection).FindOne(ctx, filter).Decode(&policy)
	return policy, err
}

// --- 分摊结果 ---

func (d *allocationDAO) InsertAllocation(ctx context.Context, alloc domain.CostAllocation) (int64, error) {
	now := time.Now().UnixMilli()
	alloc.CreateTime = now
	if alloc.ID == 0 {
		alloc.ID = d.db.GetIdGenerator(AllocationCollection)
	}
	_, err := d.db.Collection(AllocationCollection).InsertOne(ctx, alloc)
	if err != nil {
		return 0, err
	}
	return alloc.ID, nil
}

func (d *allocationDAO) InsertAllocations(ctx context.Context, allocs []domain.CostAllocation) (int64, error) {
	if len(allocs) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(allocs))
	for i := range allocs {
		allocs[i].CreateTime = now
		if allocs[i].ID == 0 {
			allocs[i].ID = d.db.GetIdGenerator(AllocationCollection)
		}
		docs[i] = allocs[i]
	}
	result, err := d.db.Collection(AllocationCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(result.InsertedIDs)), nil
}

func (d *allocationDAO) DeleteAllocationsByPeriod(ctx context.Context, tenantID string, period string) error {
	filter := bson.M{
		"tenant_id": tenantID,
		"period":    period,
	}
	_, err := d.db.Collection(AllocationCollection).DeleteMany(ctx, filter)
	return err
}

func (d *allocationDAO) ListAllocations(ctx context.Context, filter repository.AllocationFilter) ([]domain.CostAllocation, error) {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.DimType != "" {
		query["dim_type"] = filter.DimType
	}
	if filter.Period != "" {
		query["period"] = filter.Period
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "total_amount", Value: -1}})

	cursor, err := d.db.Collection(AllocationCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var allocs []domain.CostAllocation
	err = cursor.All(ctx, &allocs)
	return allocs, err
}

func (d *allocationDAO) GetAllocationByDimension(ctx context.Context, tenantID, dimType, dimValue, period string) ([]domain.CostAllocation, error) {
	filter := bson.M{
		"tenant_id": tenantID,
		"dim_type":  dimType,
		"dim_value": dimValue,
		"period":    period,
	}
	cursor, err := d.db.Collection(AllocationCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var allocs []domain.CostAllocation
	err = cursor.All(ctx, &allocs)
	return allocs, err
}

func (d *allocationDAO) GetAllocationByNode(ctx context.Context, tenantID string, nodeID int64, period string) ([]domain.CostAllocation, error) {
	filter := bson.M{
		"tenant_id": tenantID,
		"node_id":   nodeID,
		"period":    period,
	}
	cursor, err := d.db.Collection(AllocationCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var allocs []domain.CostAllocation
	err = cursor.All(ctx, &allocs)
	return allocs, err
}

func (d *allocationDAO) buildRuleQuery(filter repository.AllocationRuleFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.RuleType != "" {
		query["rule_type"] = filter.RuleType
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	return query
}
