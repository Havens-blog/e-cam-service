package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// 连接信息来自环境变量（源码不再内置生产地址与口令）
	uri := os.Getenv("ECAM_MONGODB_DSN")
	if uri == "" {
		log.Fatal("缺少连接信息：请设置环境变量 ECAM_MONGODB_DSN（脚本不再内置生产地址/口令）")
	}
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
