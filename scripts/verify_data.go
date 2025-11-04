//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://ecmdb:123456@118.145.73.93:27017/ecmdb?authSource=admin"
	}
	dbName := os.Getenv("MONGO_DATABASE")
	if dbName == "" {
		dbName = "ecmdb"
	}

	fmt.Printf("🔌 连接到 MongoDB: %s, 数据库: %s\n", uri, dbName)

	clientOptions := options.Client().ApplyURI(uri)
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

	db := mongox.NewMongo(client, dbName)
	ctx := context.Background()

	modelDAO := dao.NewModelDAO(db)
	fieldDAO := dao.NewModelFieldDAO(db)
	groupDAO := dao.NewModelFieldGroupDAO(db)

	modelRepo := repository.NewModelRepository(modelDAO)
	fieldRepo := repository.NewModelFieldRepository(fieldDAO)
	groupRepo := repository.NewModelFieldGroupRepository(groupDAO)

	fmt.Println("\n📊 验证模型数据")
	fmt.Println("================")

	models, err := modelRepo.ListModels(ctx, domain.ModelFilter{})
	if err != nil {
		fmt.Printf("❌ 查询模型失败: %v\n", err)
		return
	}

	fmt.Printf("\n✅ 找到 %d 个模型:\n", len(models))
	for _, model := range models {
		fmt.Printf("  📦 %s (%s): %s\n", model.UID, model.Name, model.Description)
	}

	// 查找 cloud_ecs 模型
	var cloudECSModel *domain.Model
	for i := range models {
		if models[i].UID == "cloud_ecs" {
			cloudECSModel = &models[i]
			break
		}
	}

	if cloudECSModel != nil {
		modelUID := cloudECSModel.UID

		fields, err := fieldRepo.GetFieldsByModelUID(ctx, modelUID)
		if err != nil {
			fmt.Printf("❌ 查询字段失败: %v\n", err)
			return
		}

		fmt.Printf("\n✅ 模型 %s 的字段 (%d 个):\n", modelUID, len(fields))
		for _, field := range fields {
			displayStatus := "❌"
			if field.Display {
				displayStatus = "✅"
			}
			fmt.Printf("  %s %s (%s): %s\n",
				displayStatus, field.FieldUID, field.FieldName, field.DisplayName)
		}

		groups, err := groupRepo.GetGroupsByModelUID(ctx, modelUID)
		if err != nil {
			fmt.Printf("❌ 查询分组失败: %v\n", err)
			return
		}

		fmt.Printf("\n✅ 模型 %s 的分组 (%d 个):\n", modelUID, len(groups))
		for _, group := range groups {
			fmt.Printf("  📁 %s (索引: %d)\n", group.Name, group.Index)
		}
	}

	fmt.Println("\n🎉 数据验证完成！")
}
