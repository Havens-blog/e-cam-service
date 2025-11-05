package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitIndexes 初始化数据库索引
func InitIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 初始化资产集合索引
	if err := initAssetIndexes(ctx, db); err != nil {
		return err
	}

	// 初始化云账号集合索引
	if err := initAccountIndexes(ctx, db); err != nil {
		return err
	}

	// 初始化模型集合索引
	if err := initModelIndexes(ctx, db); err != nil {
		return err
	}

	// 初始化字段集合索引
	if err := initFieldIndexes(ctx, db); err != nil {
		return err
	}

	// 初始化字段分组集合索引
	if err := initFieldGroupIndexes(ctx, db); err != nil {
		return err
	}

	return nil
}

// initAssetIndexes 初始化资产集合索引
func initAssetIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(AssetCollection)

	// 创建资产索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "asset_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "provider", Value: 1},
				{Key: "asset_type", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "region", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "asset_name", Value: "text"},
			},
		},
		{
			Keys: bson.D{
				{Key: "ctime", Value: -1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initAccountIndexes 初始化云账号集合索引
func initAccountIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(AccountsCollection)

	// 创建云账号索引
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "name", Value: 1},
				{Key: "tenant_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "provider", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "environment", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "ctime", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "last_sync_time", Value: -1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initModelIndexes 初始化模型集合索引
func initModelIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(ModelCollection)

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "uid", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "model_group_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "provider", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "category", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "parent_uid", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "level", Value: 1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initFieldIndexes 初始化字段集合索引
func initFieldIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(FieldCollection)

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "field_uid", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "model_uid", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "model_uid", Value: 1},
				{Key: "index", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "group_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "field_type", Value: 1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initFieldGroupIndexes 初始化字段分组集合索引
func initFieldGroupIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(FieldGroupCollection)

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "model_uid", Value: 1},
				{Key: "index", Value: 1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
