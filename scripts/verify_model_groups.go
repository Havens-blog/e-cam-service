//go:build ignore
// +build ignore

// +build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	uri := "mongodb://ecmdb:123456@118.145.73.93:27017/ecmdb?authSource=admin"
	dbName := "ecmdb"

	fmt.Printf("🔌 连接�?MongoDB: %s, 数据�? %s\n", uri, dbName)

	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		fmt.Printf("�?连接数据库失�? %v\n", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	db := mongox.NewMongo(client, dbName)
	ctx := context.Background()

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 验证模型分组关联")
	fmt.Println(strings.Repeat("=", 80))

	// 查询模型分组
	modelGroupDAO := dao.NewModelGroupDAO(db)
	groups, err := modelGroupDAO.List(ctx)
	if err != nil {
		fmt.Printf("�?查询模型分组失败: %v\n", err)
		return
	}

	fmt.Printf("\n【模型分组】\n")
	groupMap := make(map[int64]string)
	for _, group := range groups {
		groupMap[group.ID] = group.Name
		fmt.Printf("  %d. %s\n", group.ID, group.Name)
	}

	// 查询模型
	modelDAO := dao.NewModelDAO(db)
	models, err := modelDAO.ListModels(ctx, dao.ModelFilter{})
	if err != nil {
		fmt.Printf("�?查询模型失败: %v\n", err)
		return
	}

	fmt.Printf("\n【模型及其分组关联】\n")
	for _, model := range models {
		groupName := groupMap[model.ModelGroupID]
		if groupName == "" {
			groupName = fmt.Sprintf("�?未找到分�?(ID: %d)", model.ModelGroupID)
		} else {
			groupName = fmt.Sprintf("�?%s", groupName)
		}
		fmt.Printf("  - %s (%s)\n", model.Name, model.UID)
		fmt.Printf("    分类: %s\n", model.Category)
		fmt.Printf("    分组: %s\n", groupName)
		fmt.Printf("    model_group_id: %d\n\n", model.ModelGroupID)
	}

	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("🎉 验证完成�?)
	fmt.Println(strings.Repeat("=", 80))
}
