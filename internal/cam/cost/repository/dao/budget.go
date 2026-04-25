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

const BudgetCollection = "ecam_cost_budget"

type budgetDAO struct {
	db *mongox.Mongo
}

// NewBudgetDAO 创建预算规则 DAO
func NewBudgetDAO(db *mongox.Mongo) repository.BudgetDAO {
	return &budgetDAO{db: db}
}

func (d *budgetDAO) Create(ctx context.Context, budget domain.BudgetRule) (int64, error) {
	now := time.Now().UnixMilli()
	budget.CreateTime = now
	budget.UpdateTime = now
	if budget.ID == 0 {
		budget.ID = d.db.GetIdGenerator(BudgetCollection)
	}
	_, err := d.db.Collection(BudgetCollection).InsertOne(ctx, budget)
	if err != nil {
		return 0, err
	}
	return budget.ID, nil
}

func (d *budgetDAO) Update(ctx context.Context, budget domain.BudgetRule) error {
	budget.UpdateTime = time.Now().UnixMilli()
	filter := bson.M{"id": budget.ID}
	update := bson.M{"$set": bson.M{
		"name":         budget.Name,
		"amount_limit": budget.AmountLimit,
		"period":       budget.Period,
		"scope_type":   budget.ScopeType,
		"scope_value":  budget.ScopeValue,
		"thresholds":   budget.Thresholds,
		"status":       budget.Status,
		"utime":        budget.UpdateTime,
	}}
	result, err := d.db.Collection(BudgetCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (d *budgetDAO) GetByID(ctx context.Context, id int64) (domain.BudgetRule, error) {
	var budget domain.BudgetRule
	filter := bson.M{"id": id}
	err := d.db.Collection(BudgetCollection).FindOne(ctx, filter).Decode(&budget)
	return budget, err
}

func (d *budgetDAO) List(ctx context.Context, filter repository.BudgetFilter) ([]domain.BudgetRule, error) {
	query := d.buildQuery(filter)
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(BudgetCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var budgets []domain.BudgetRule
	err = cursor.All(ctx, &budgets)
	return budgets, err
}

func (d *budgetDAO) Count(ctx context.Context, filter repository.BudgetFilter) (int64, error) {
	query := d.buildQuery(filter)
	return d.db.Collection(BudgetCollection).CountDocuments(ctx, query)
}

func (d *budgetDAO) ListActive(ctx context.Context, tenantID string) ([]domain.BudgetRule, error) {
	filter := bson.M{
		"tenant_id": tenantID,
		"status":    "active",
	}
	cursor, err := d.db.Collection(BudgetCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var budgets []domain.BudgetRule
	err = cursor.All(ctx, &budgets)
	return budgets, err
}

func (d *budgetDAO) UpdateStatus(ctx context.Context, id int64, status string) error {
	filter := bson.M{"id": id}
	update := bson.M{"$set": bson.M{
		"status": status,
		"utime":  time.Now().UnixMilli(),
	}}
	_, err := d.db.Collection(BudgetCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *budgetDAO) UpdateNotifiedAt(ctx context.Context, id int64, notifiedAt map[string]time.Time) error {
	filter := bson.M{"id": id}
	update := bson.M{"$set": bson.M{
		"notified_at": notifiedAt,
		"utime":       time.Now().UnixMilli(),
	}}
	_, err := d.db.Collection(BudgetCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *budgetDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := d.db.Collection(BudgetCollection).DeleteOne(ctx, filter)
	return err
}

func (d *budgetDAO) buildQuery(filter repository.BudgetFilter) bson.M {
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.ScopeType != "" {
		query["scope_type"] = filter.ScopeType
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	return query
}
