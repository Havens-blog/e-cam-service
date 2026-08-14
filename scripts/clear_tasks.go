package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// MongoDB 连接配置
	uri := "mongodb://ecmdb:123456@106.52.187.69:27017/?authSource=admin"
	databaseName := "ecam"
	collectionName := "ecam_task"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 连接 MongoDB
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("连接 MongoDB 失败: %v", err)
	}
	defer client.Disconnect(ctx)

	// 获取集合
	collection := client.Database(databaseName).Collection(collectionName)

	// 先统计数量
	count, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计任务数量失败: %v", err)
	}

	fmt.Printf("当前共有 %d 条任务\n", count)

	if count == 0 {
		fmt.Println("没有需要删除的任务")
		return
	}

	// 确认删除
	fmt.Print("确认删除所有任务？(yes/no): ")
	var confirm string
	fmt.Scanln(&confirm)

	if confirm != "yes" && confirm != "y" {
		fmt.Println("取消删除")
		return
	}

	// 删除所有任务
	result, err := collection.DeleteMany(ctx, bson.M{})
	if err != nil {
		log.Fatalf("删除任务失败: %v", err)
	}

	fmt.Printf("成功删除 %d 条任务\n", result.DeletedCount)
}
