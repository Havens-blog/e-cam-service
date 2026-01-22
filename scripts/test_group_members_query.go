package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

func main() {
	// 连接数据库
	db, err := mongox.NewMongo(&mongox.Config{
		DSN:      "mongodb://admin:Aa123456@localhost:27017",
		Database: "e-cam-service",
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	ctx := context.Background()

	// 测试查询用户组成员
	groupID := int64(1) // 替换为实际的用户组ID
	tenantID := "tenant-001"

	fmt.Printf("查询用户组 %d 的成员 (tenant_id: %s)\n", groupID, tenantID)
	fmt.Println("=" + string(make([]byte, 60)))

	// 方法1: 原来的错误方法 - 查询所有用户再筛选
	fmt.Println("\n方法1: 查询所有用户再筛选 (错误方法)")
	filter1 := bson.M{"tenant_id": tenantID}
	cursor1, err := db.Collection(dao.CloudIAMUsersCollection).Find(ctx, filter1)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	defer cursor1.Close(ctx)

	var allUsers []dao.CloudUser
	if err := cursor1.All(ctx, &allUsers); err != nil {
		log.Fatalf("解析失败: %v", err)
	}

	var members1 []dao.CloudUser
	for _, user := range allUsers {
		for _, gid := range user.PermissionGroups {
			if gid == groupID {
				members1 = append(members1, user)
				break
			}
		}
	}

	fmt.Printf("查询到所有用户: %d 个\n", len(allUsers))
	fmt.Printf("筛选后成员: %d 个\n", len(members1))
	for i, member := range members1 {
		fmt.Printf("  %d. %s (ID: %d, Groups: %v)\n", i+1, member.Username, member.ID, member.PermissionGroups)
	}

	// 方法2: 正确方法 - 直接查询包含该用户组的用户
	fmt.Println("\n方法2: 直接查询包含该用户组的用户 (正确方法)")
	filter2 := bson.M{
		"permission_groups": groupID,
		"tenant_id":         tenantID,
	}
	cursor2, err := db.Collection(dao.CloudIAMUsersCollection).Find(ctx, filter2)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	defer cursor2.Close(ctx)

	var members2 []dao.CloudUser
	if err := cursor2.All(ctx, &members2); err != nil {
		log.Fatalf("解析失败: %v", err)
	}

	fmt.Printf("直接查询到成员: %d 个\n", len(members2))
	for i, member := range members2 {
		fmt.Printf("  %d. %s (ID: %d, Groups: %v)\n", i+1, member.Username, member.ID, member.PermissionGroups)
	}

	// 验证两种方法结果是否一致
	fmt.Println("\n结果对比:")
	if len(members1) == len(members2) {
		fmt.Printf("✅ 两种方法查询到的成员数量一致: %d 个\n", len(members1))
	} else {
		fmt.Printf("❌ 两种方法查询到的成员数量不一致: 方法1=%d, 方法2=%d\n", len(members1), len(members2))
	}

	// 检查数据库索引
	fmt.Println("\n检查索引:")
	indexes := db.Collection(dao.CloudIAMUsersCollection).Indexes()
	cursor3, err := indexes.List(ctx)
	if err != nil {
		log.Printf("获取索引列表失败: %v", err)
	} else {
		defer cursor3.Close(ctx)
		var indexList []bson.M
		if err := cursor3.All(ctx, &indexList); err != nil {
			log.Printf("解析索引列表失败: %v", err)
		} else {
			hasGroupIndex := false
			for _, index := range indexList {
				if key, ok := index["key"].(bson.M); ok {
					if _, exists := key["permission_groups"]; exists {
						hasGroupIndex = true
						fmt.Printf("✅ 找到 permission_groups 索引: %v\n", index["name"])
					}
				}
			}
			if !hasGroupIndex {
				fmt.Println("⚠️  未找到 permission_groups 索引，建议创建以提升查询性能")
				fmt.Println("   创建索引命令: db.cloud_iam_users.createIndex({\"permission_groups\": 1, \"tenant_id\": 1})")
			}
		}
	}
}
