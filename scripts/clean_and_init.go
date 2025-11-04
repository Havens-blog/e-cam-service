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

	// 清理 cloud_ecs 相关的旧数据
	fmt.Println("\n🧹 清理旧数据...")

	// 删除 cloud_ecs 模型
	result, err := db.Collection("c_model").DeleteMany(ctx, bson.M{"uid": "cloud_ecs"})
	if err != nil {
		fmt.Printf("⚠️  删除模型失败: %v\n", err)
	} else {
		fmt.Printf("✅ 删除了 %d 个模型记录\n", result.DeletedCount)
	}

	// 删除 cloud_ecs 的字段
	result, err = db.Collection("c_attribute").DeleteMany(ctx, bson.M{"model_uid": "cloud_ecs"})
	if err != nil {
		fmt.Printf("⚠️  删除字段失败: %v\n", err)
	} else {
		fmt.Printf("✅ 删除了 %d 个字段记录\n", result.DeletedCount)
	}

	// 删除 cloud_ecs 的分组
	result, err = db.Collection("c_attribute_group").DeleteMany(ctx, bson.M{"model_uid": "cloud_ecs"})
	if err != nil {
		fmt.Printf("⚠️  删除分组失败: %v\n", err)
	} else {
		fmt.Printf("✅ 删除了 %d 个分组记录\n", result.DeletedCount)
	}

	fmt.Println("\n✅ 清理完成！现在可以运行 init_models.go 重新导入数据")
}
