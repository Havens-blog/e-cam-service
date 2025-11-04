//go:build ignore
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

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 验证所有数据")
	fmt.Println(strings.Repeat("=", 80))

	// 1. 验证模型分组
	fmt.Println("\n【1. 模型分组 (c_model_group)】")
	modelGroupDAO := dao.NewModelGroupDAO(db)
	groups, err := modelGroupDAO.List(ctx)
	if err != nil {
		fmt.Printf("❌ 查询失败: %v\n", err)
	} else {
		fmt.Printf("✅ 找到 %d 个模型分组:\n", len(groups))
		for _, group := range groups {
			fmt.Printf("  %d. %s\n", group.ID, group.Name)
		}
	}

	// 2. 验证关系类型
	fmt.Println("\n【2. 关系类型 (c_relation_type)】")
	relationTypeDAO := dao.NewRelationTypeDAO(db)
	relationTypes, err := relationTypeDAO.List(ctx)
	if err != nil {
		fmt.Printf("❌ 查询失败: %v\n", err)
	} else {
		fmt.Printf("✅ 找到 %d 个关系类型:\n", len(relationTypes))
		for _, rt := range relationTypes {
			fmt.Printf("  - %s (%s): %s -> %s\n",
				rt.Name, rt.UID, rt.SourceDescribe, rt.TargetDescribe)
		}
	}

	// 3. 验证模型
	fmt.Println("\n【3. 模型 (c_model)】")
	modelDAO := dao.NewModelDAO(db)
	models, err := modelDAO.ListModels(ctx, dao.ModelFilter{})
	if err != nil {
		fmt.Printf("❌ 查询失败: %v\n", err)
	} else {
		fmt.Printf("✅ 找到 %d 个模型:\n", len(models))
		for _, model := range models {
			fmt.Printf("  - %s (%s): %s\n", model.Name, model.UID, model.Description)
		}
	}

	// 4. 验证字段
	fmt.Println("\n【4. 字段 (c_attribute)】")
	fieldDAO := dao.NewModelFieldDAO(db)
	allFields := 0
	for _, model := range models {
		fields, err := fieldDAO.GetFieldsByModelUID(ctx, model.UID)
		if err != nil {
			fmt.Printf("  ⚠️  模型 %s 查询字段失败: %v\n", model.UID, err)
			continue
		}
		allFields += len(fields)
	}
	fmt.Printf("✅ 总共 %d 个字段\n", allFields)

	// 5. 验证字段分组
	fmt.Println("\n【5. 字段分组 (c_attribute_group)】")
	groupDAO := dao.NewModelFieldGroupDAO(db)
	allGroups := 0
	for _, model := range models {
		groups, err := groupDAO.GetGroupsByModelUID(ctx, model.UID)
		if err != nil {
			fmt.Printf("  ⚠️  模型 %s 查询分组失败: %v\n", model.UID, err)
			continue
		}
		allGroups += len(groups)
	}
	fmt.Printf("✅ 总共 %d 个字段分组\n", allGroups)

	// 6. 验证模型关系
	fmt.Println("\n【6. 模型关系 (c_relation_model)】")
	modelRelationDAO := dao.NewModelRelationDAO(db)
	relations, err := modelRelationDAO.List(ctx)
	if err != nil {
		fmt.Printf("❌ 查询失败: %v\n", err)
	} else {
		fmt.Printf("✅ 找到 %d 个模型关系:\n", len(relations))
		for _, rel := range relations {
			fmt.Printf("  - %s: %s -> %s (%s)\n",
				rel.RelationName, rel.SourceModelUID, rel.TargetModelUID, rel.Mapping)
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("🎉 所有数据验证完成！")
	fmt.Println(strings.Repeat("=", 80))

	// 统计信息
	fmt.Printf("\n📈 统计信息:\n")
	fmt.Printf("  模型分组:   %d\n", len(groups))
	fmt.Printf("  关系类型:   %d\n", len(relationTypes))
	fmt.Printf("  模型:       %d\n", len(models))
	fmt.Printf("  字段:       %d\n", allFields)
	fmt.Printf("  字段分组:   %d\n", allGroups)
	fmt.Printf("  模型关系:   %d\n", len(relations))
}
