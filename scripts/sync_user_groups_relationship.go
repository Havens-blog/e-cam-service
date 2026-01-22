package main

import (
	"context"
	"fmt"
	"log"

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

	fmt.Println("========================================")
	fmt.Println("同步用户-用户组关系")
	fmt.Println("========================================")
	fmt.Println()

	// 1. 查询所有用户组
	fmt.Println("1. 查询所有用户组")
	fmt.Println("----------------------------------------")
	
	cursor, err := db.Collection("cloud_iam_groups").Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("查询用户组失败: %v", err)
	}
	defer cursor.Close(ctx)

	var groups []bson.M
	if err := cursor.All(ctx, &groups); err != nil {
		log.Fatalf("解析用户组失败: %v", err)
	}

	fmt.Printf("找到 %d 个用户组\n\n", len(groups))

	if len(groups) == 0 {
		fmt.Println("⚠️  没有找到用户组，请先同步用户组")
		fmt.Println()
		fmt.Println("同步命令:")
		fmt.Println("  curl -X POST \"http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1\" \\")
		fmt.Println("    -H \"X-Tenant-ID: tenant-001\"")
		return
	}

	// 2. 检查每个用户组的云平台ID
	fmt.Println("2. 检查用户组的云平台ID")
	fmt.Println("----------------------------------------")
	
	groupsWithCloudID := 0
	groupsWithoutCloudID := 0
	
	for i, group := range groups {
		groupName := "unknown"
		if name, ok := group["name"].(string); ok {
			groupName = name
		}
		
		groupID := int64(0)
		if id, ok := group["id"].(int64); ok {
			groupID = id
		} else if id, ok := group["id"].(int32); ok {
			groupID = int64(id)
		}
		
		cloudGroupID := ""
		if cgid, ok := group["cloud_group_id"].(string); ok {
			cloudGroupID = cgid
		}
		
		if cloudGroupID != "" {
			fmt.Printf("  %d. ✅ 用户组 %s (ID: %d) 有云平台ID: %s\n", i+1, groupName, groupID, cloudGroupID)
			groupsWithCloudID++
		} else {
			fmt.Printf("  %d. ⚠️  用户组 %s (ID: %d) 没有云平台ID\n", i+1, groupName, groupID)
			groupsWithoutCloudID++
		}
	}
	
	fmt.Println()
	fmt.Printf("统计: 有云平台ID的用户组 %d 个, 无云平台ID的用户组 %d 个\n\n", groupsWithCloudID, groupsWithoutCloudID)

	if groupsWithoutCloudID > 0 {
		fmt.Println("⚠️  注意: 没有云平台ID的用户组无法自动同步成员关系")
		fmt.Println("   这些用户组可能是手动创建的，需要手动分配成员")
		fmt.Println()
	}

	// 3. 检查用户的 permission_groups 字段
	fmt.Println("3. 检查用户的 permission_groups 字段")
	fmt.Println("----------------------------------------")
	
	cursor2, err := db.Collection("cloud_iam_users").Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("查询用户失败: %v", err)
	}
	defer cursor2.Close(ctx)

	var users []bson.M
	if err := cursor2.All(ctx, &users); err != nil {
		log.Fatalf("解析用户失败: %v", err)
	}

	fmt.Printf("找到 %d 个用户\n\n", len(users))

	usersWithGroups := 0
	usersWithoutGroups := 0
	usersNeedInit := 0
	
	for _, user := range users {
		permissionGroups, hasField := user["permission_groups"]
		
		if !hasField {
			usersNeedInit++
			usersWithoutGroups++
		} else if groups, ok := permissionGroups.(bson.A); ok {
			if len(groups) > 0 {
				usersWithGroups++
			} else {
				usersWithoutGroups++
			}
		} else {
			usersNeedInit++
			usersWithoutGroups++
		}
	}
	
	fmt.Printf("有用户组的用户: %d 个\n", usersWithGroups)
	fmt.Printf("无用户组的用户: %d 个\n", usersWithoutGroups)
	fmt.Printf("需要初始化的用户: %d 个\n\n", usersNeedInit)

	// 4. 初始化缺失的 permission_groups 字段
	if usersNeedInit > 0 {
		fmt.Println("4. 初始化缺失的 permission_groups 字段")
		fmt.Println("----------------------------------------")
		
		result, err := db.Collection("cloud_iam_users").UpdateMany(
			ctx,
			bson.M{
				"$or": []bson.M{
					{"permission_groups": bson.M{"$exists": false}},
					{"permission_groups": nil},
				},
			},
			bson.M{
				"$set": bson.M{
					"permission_groups": bson.A{},
				},
			},
		)
		
		if err != nil {
			fmt.Printf("❌ 初始化失败: %v\n", err)
		} else {
			fmt.Printf("✅ 成功初始化 %d 个用户的 permission_groups 字段\n", result.ModifiedCount)
		}
		fmt.Println()
	}

	// 5. 建议操作
	fmt.Println("5. 建议操作")
	fmt.Println("----------------------------------------")
	fmt.Println()
	fmt.Println("✅ 数据已初始化，现在需要重新同步用户组以建立成员关系:")
	fmt.Println()
	fmt.Println("方法 1: 通过 API 同步（推荐）")
	fmt.Println("  curl -X POST \"http://localhost:8080/api/v1/cam/iam/groups/sync?cloud_account_id=1\" \\")
	fmt.Println("    -H \"X-Tenant-ID: tenant-001\"")
	fmt.Println()
	fmt.Println("方法 2: 如果用户组是手动创建的，需要手动分配成员")
	fmt.Println("  curl -X POST \"http://localhost:8080/api/v1/cam/iam/users/assign-groups\" \\")
	fmt.Println("    -H \"X-Tenant-ID: tenant-001\" \\")
	fmt.Println("    -H \"Content-Type: application/json\" \\")
	fmt.Println("    -d '{\"user_ids\": [1,2,3], \"group_ids\": [1]}'")
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("完成")
	fmt.Println("========================================")
}
