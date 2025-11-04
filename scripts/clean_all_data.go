//go:build ignore
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

	fmt.Printf("🔌 连接到 MongoDB 数据库: %s\n", mongoDatabase)

	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		fmt.Printf("❌ 连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(context.Background(), nil); err != nil {
		fmt.Printf("❌ Ping 数据库失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 数据库连接成功")

	db := mongox.NewMongo(client, mongoDatabase)
	ctx := context.Background()

	fmt.Println("\n🧹 清理所有云资源相关数据...")

	collections := []string{
		"c_model",
		"c_attribute",
		"c_attribute_group",
		"c_model_group",
		"c_relation_type",
		"c_relation_model",
	}

	for _, collName := range collections {
		result, err := db.Collection(collName).DeleteMany(ctx, bson.M{})
		if err != nil {
			fmt.Printf("⚠️  清理集合 %s 失败: %v\n", collName, err)
		} else {
			fmt.Printf("✅ 清理集合 %s: 删除了 %d 条记录\n", collName, result.DeletedCount)
		}
	}

	fmt.Println("\n✅ 清理完成！现在可以运行 init_models.go 重新导入数据")
}
