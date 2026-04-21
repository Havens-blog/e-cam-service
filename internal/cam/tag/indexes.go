package tag

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitIndexes 初始化标签管理模块的 MongoDB 索引
func InitIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := initTagPolicyIndexes(ctx, db); err != nil {
		return err
	}
	return initTagRuleIndexes(ctx, db)
}

// initTagPolicyIndexes 初始化标签策略集合索引
func initTagPolicyIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(TagPolicyCollection)
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
				{Key: "name", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initTagRuleIndexes 初始化自动打标规则集合索引
func initTagRuleIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(TagRuleCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
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
