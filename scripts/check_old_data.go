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

	db := mongox.NewMongo(client, mongoDatabase)
	ctx := context.Background()

	// 查询所有模型
	fmt.Println("\n📊 查询现有模型...")
	cursor, err := db.Collection("c_model").Find(ctx, bson.M{})
	if err != nil {
		fmt.Printf("❌ 查询失败: %v\n", err)
		return
	}
	defer cursor.Close(ctx)

	var models []bson.M
	if err = cursor.All(ctx, &models); err != nil {
		fmt.Printf("❌ 解码失败: %v\n", err)
		return
	}

	fmt.Printf("\n找到 %d 个模型:\n", len(models))
	for _, model := range models {
		fmt.Printf("\n模型: %v\n", model["uid"])
		fmt.Printf("  名称: %v\n", model["name"])
		fmt.Printf("  描述: %v\n", model["description"])
		
		// 查询该模型的字段
		modelUID := model["uid"]
		fieldCursor, err := db.Collection("c_attribute").Find(ctx, bson.M{"model_uid": modelUID})
		if err != nil {
			fmt.Printf("  ⚠️  查询字段失败: %v\n", err)
			continue
		}
		
		var fields []bson.M
		if err = fieldCursor.All(ctx, &fields); err != nil {
			fmt.Printf("  ⚠️  解码字段失败: %v\n", err)
			fieldCursor.Close(ctx)
			continue
		}
		fieldCursor.Close(ctx)
		
		fmt.Printf("  字段数量: %d\n", len(fields))
		if len(fields) > 0 {
			fmt.Println("  前3个字段:")
			for i, field := range fields {
				if i >= 3 {
					break
				}
				fmt.Printf("    - %v (%v): display=%v, link=%v\n", 
					field["field_uid"], field["field_name"], field["display"], field["link"])
			}
		}
	}
}
