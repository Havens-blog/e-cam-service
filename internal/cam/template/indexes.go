package template

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitIndexes 初始化主机模板模块的 MongoDB 索引
func InitIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := initTemplateIndexes(ctx, db); err != nil {
		return err
	}
	if err := initProvisionTaskIndexes(ctx, db); err != nil {
		return err
	}
	return nil
}

// initTemplateIndexes 初始化 vm_templates 集合索引
func initTemplateIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(VMTemplateCollection)
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
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "provider", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "cloud_account_id", Value: 1},
			},
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initProvisionTaskIndexes 初始化 provision_tasks 集合索引
func initProvisionTaskIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(ProvisionTaskCollection)

	// TTL: 90 天过期
	ttlSeconds := int32(90 * 24 * 3600)

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "template_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "status", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "source", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "source", Value: 1},
				{Key: "status", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "ctime", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "ctime", Value: 1},
			},
			Options: options.Index().SetExpireAfterSeconds(ttlSeconds),
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
