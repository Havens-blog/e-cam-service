package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitCostIndexes 初始化成本模块所有集合的 MongoDB 索引
func InitCostIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := initRawBillIndexes(ctx, db); err != nil {
		return err
	}
	if err := initUnifiedBillIndexes(ctx, db); err != nil {
		return err
	}
	if err := initCollectLogIndexes(ctx, db); err != nil {
		return err
	}
	if err := initBudgetIndexes(ctx, db); err != nil {
		return err
	}
	if err := initAllocationIndexes(ctx, db); err != nil {
		return err
	}
	if err := initAllocationRuleIndexes(ctx, db); err != nil {
		return err
	}
	if err := initAnomalyIndexes(ctx, db); err != nil {
		return err
	}
	if err := initRecommendationIndexes(ctx, db); err != nil {
		return err
	}
	return nil
}

// initRawBillIndexes 初始化原始账单集合索引
func initRawBillIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(RawBillCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "account_id", Value: 1},
				{Key: "billing_date", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "collect_id", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initUnifiedBillIndexes 初始化统一账单集合索引
func initUnifiedBillIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(UnifiedBillCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "billing_date", Value: 1},
				{Key: "provider", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "account_id", Value: 1},
				{Key: "billing_date", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "service_type", Value: 1},
				{Key: "billing_date", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "region", Value: 1},
				{Key: "billing_date", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "resource_id", Value: 1},
				{Key: "billing_date", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initCollectLogIndexes 初始化采集日志集合索引
func initCollectLogIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(CollectLogCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "account_id", Value: 1},
				{Key: "status", Value: 1},
				{Key: "ctime", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "ctime", Value: -1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initBudgetIndexes 初始化预算规则集合索引
func initBudgetIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(BudgetCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "status", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initAllocationIndexes 初始化成本分摊结果集合索引
func initAllocationIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(AllocationCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "period", Value: 1},
				{Key: "dim_type", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "node_id", Value: 1},
				{Key: "period", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initAllocationRuleIndexes 初始化分摊规则集合索引
func initAllocationRuleIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(AllocationRuleCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "status", Value: 1},
				{Key: "priority", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initAnomalyIndexes 初始化异常事件集合索引
func initAnomalyIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(AnomalyCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "anomaly_date", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "dimension", Value: 1},
				{Key: "anomaly_date", Value: -1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initRecommendationIndexes 初始化优化建议集合索引
func initRecommendationIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(RecommendationCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "status", Value: 1},
				{Key: "type", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "resource_id", Value: 1},
				{Key: "type", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
