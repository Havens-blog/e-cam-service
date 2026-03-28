package dictionary

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InitIndexes 初始化数据字典模块的 MongoDB 索引
func InitIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := initDictTypeIndexes(ctx, db); err != nil {
		return err
	}
	if err := initDictItemIndexes(ctx, db); err != nil {
		return err
	}
	return nil
}

// initDictTypeIndexes 初始化字典类型集合索引
func initDictTypeIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(DictTypeCollection)
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
				{Key: "code", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// initDictItemIndexes 初始化字典项集合索引
func initDictItemIndexes(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(DictItemCollection)
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "dict_type_id", Value: 1},
				{Key: "value", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
