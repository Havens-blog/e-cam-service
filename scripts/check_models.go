// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	mongoURI := "mongodb://ecmdb:123456@118.145.73.93:27017/ecmdb?authSource=admin"
	mongoDatabase := "ecmdb"

	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		fmt.Printf("❌ 连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	db := mongox.NewMongo(client, mongoDatabase)
	ctx := context.Background()

	// 检查所有集合
	collections := []string{"c_model", "c_attribute", "c_attribute_group"}
	
	for _, collName := range collections {
		fmt.Printf("\n📊 集合: %s\n", collName)
		count, err := db.Collection(collName).CountDocuments(ctx, bson.M{})
		if err != nil {
			fmt.Printf("  ❌ 查询失败: %v\n", err)
			continue
		}
		fmt.Printf("  文档数量: %d\n", count)
		
		// 查询所有不同的 model_uid 或 uid
		var pipeline mongo.Pipeline
		if collName == "c_model" {
			pipeline = mongo.Pipeline{
				{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$uid"}}}},
			}
		} else {
			pipeline = mongo.Pipeline{
				{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$model_uid"}}}},
			}
		}
		
		cursor, err := db.Collection(collName).Aggregate(ctx, pipeline)
		if err != nil {
			fmt.Printf("  ⚠️  聚合查询失败: %v\n", err)
			continue
		}
		
		var results []bson.M
		if err = cursor.All(ctx, &results); err != nil {
			fmt.Printf("  ⚠️  解码失败: %v\n", err)
			cursor.Close(ctx)
			continue
		}
		cursor.Close(ctx)
		
		fmt.Printf("  模型列表:\n")
		for _, result := range results {
			fmt.Printf("    - %v\n", result["_id"])
		}
	}
}
