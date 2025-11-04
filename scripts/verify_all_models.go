//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
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

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 验证所有云资源模型")
	fmt.Println(strings.Repeat("=", 80))

	models, err := modelRepo.ListModels(ctx, domain.ModelFilter{})
	if err != nil {
		fmt.Printf("❌ 查询模型失败: %v\n", err)
		return
	}

	fmt.Printf("\n✅ 找到 %d 个模型\n", len(models))

	for i, model := range models {
		fmt.Printf("\n" + strings.Repeat("-", 80) + "\n")
		fmt.Printf("模型 %d: %s\n", i+1, model.Name)
		fmt.Printf(strings.Repeat("-", 80) + "\n")
		fmt.Printf("  UID:         %s\n", model.UID)
		fmt.Printf("  分类:        %s\n", model.Category)
		fmt.Printf("  描述:        %s\n", model.Description)
		fmt.Printf("  图标:        %s\n", model.Icon)
		fmt.Printf("  云厂商:      %s\n", model.Provider)

		// 查询字段
		fields, err := fieldRepo.GetFieldsByModelUID(ctx, model.UID)
		if err != nil {
			fmt.Printf("  ❌ 查询字段失败: %v\n", err)
			continue
		}

		// 查询分组
		groups, err := groupRepo.GetGroupsByModelUID(ctx, model.UID)
		if err != nil {
			fmt.Printf("  ❌ 查询分组失败: %v\n", err)
			continue
		}

		fmt.Printf("\n  📁 字段分组 (%d 个):\n", len(groups))
		for _, group := range groups {
			fmt.Printf("    %d. %s\n", group.Index, group.Name)
		}

		fmt.Printf("\n  📋 字段列表 (%d 个):\n", len(fields))
		
		// 按分组显示字段
		groupMap := make(map[int64]string)
		for _, group := range groups {
			groupMap[group.ID] = group.Name
		}

		currentGroup := ""
		for _, field := range fields {
			groupName := groupMap[field.GroupID]
			if groupName != currentGroup {
				fmt.Printf("\n    【%s】\n", groupName)
				currentGroup = groupName
			}

			displayStatus := "✅"
			if !field.Display {
				displayStatus = "❌"
			}
			requiredMark := ""
			if field.Required {
				requiredMark = " *"
			}

			fmt.Printf("      %s %s (%s)%s\n", 
				displayStatus, field.DisplayName, field.FieldName, requiredMark)
			fmt.Printf("         类型: %s, UID: %s\n", 
				field.FieldType, field.FieldUID)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("🎉 所有模型验证完成！")
	fmt.Println(strings.Repeat("=", 80))
	
	// 统计信息
	totalFields := 0
	totalGroups := 0
	for _, model := range models {
		fields, _ := fieldRepo.GetFieldsByModelUID(ctx, model.UID)
		groups, _ := groupRepo.GetGroupsByModelUID(ctx, model.UID)
		totalFields += len(fields)
		totalGroups += len(groups)
	}
	
	fmt.Printf("\n📈 统计信息:\n")
	fmt.Printf("  模型总数:   %d\n", len(models))
	fmt.Printf("  字段总数:   %d\n", totalFields)
	fmt.Printf("  分组总数:   %d\n", totalGroups)
	fmt.Printf("  平均字段数: %.1f 个/模型\n", float64(totalFields)/float64(len(models)))
}
