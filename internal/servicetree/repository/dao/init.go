package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitIndexes 初始化服务树相关索引
func InitIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := initNodeIndexes(ctx, db); err != nil {
		return err
	}
	if err := initBindingIndexes(ctx, db); err != nil {
		return err
	}
	if err := initRuleIndexes(ctx, db); err != nil {
		return err
	}
	if err := initEnvironmentIndexes(ctx, db); err != nil {
		return err
	}

	return nil
}

func initNodeIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(NodeCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "uid", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "parent_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "path", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "level", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

func initBindingIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(BindingCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			// 资源唯一绑定：同一资源只能绑定到一个节点
			Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "resource_type", Value: 1}, {Key: "resource_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "node_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "node_id", Value: 1}, {Key: "env_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "env_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "rule_id", Value: 1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

func initRuleIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(RuleCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "node_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "enabled", Value: 1}, {Key: "priority", Value: 1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

func initEnvironmentIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(EnvironmentCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "code", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
