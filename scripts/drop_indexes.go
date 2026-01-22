package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	uri := "mongodb://ecmdb:123456@118.145.73.93:27017/ecmdb?authSource=admin"
	dbName := "ecmdb"

	fmt.Printf("🔌 连接到 MongoDB: %s, 数据库: %s\n", uri, dbName)

	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(context.Background())

	db := mongox.NewMongo(client, "ecam")
	ctx := context.Background()

	// 需要清理的集合
	collections := []string{
		"cloud_iam_users",
		"cloud_permission_groups",
		"cloud_sync_tasks",
		"cloud_audit_logs",
		"cloud_policy_templates",
		"tenants",
	}

	for _, collName := range collections {
		fmt.Printf("处理集合: %s\n", collName)
		collection := db.Collection(collName)

		// 列出所有索引
		cursor, err := collection.Indexes().List(ctx)
		if err != nil {
			log.Printf("列出索引失败 %s: %v\n", collName, err)
			continue
		}

		var indexes []bson.M
		if err = cursor.All(ctx, &indexes); err != nil {
			log.Printf("读取索引失败 %s: %v\n", collName, err)
			continue
		}

		// 删除除了 _id_ 之外的所有索引
		for _, index := range indexes {
			indexName := index["name"].(string)
			if indexName != "_id_" {
				fmt.Printf("  删除索引: %s\n", indexName)
				_, err := collection.Indexes().DropOne(ctx, indexName)
				if err != nil {
					log.Printf("  删除索引失败 %s: %v\n", indexName, err)
				}
			}
		}
	}

	fmt.Println("\n索引清理完成！现在可以重新启动服务来创建新索引。")
}
