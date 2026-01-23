//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
)

func main() {
	// 连接数据�?
	db, err := mongox.NewMongo(&mongox.Config{
		DSN:      "mongodb://admin:Aa123456@localhost:27017",
		Database: "e-cam-service",
	})
	if err != nil {
		log.Fatalf("连接数据库失�? %v", err)
	}

	ctx := context.Background()

	fmt.Println("========================================")
	fmt.Println("用户组成员关系修�?)
	fmt.Println("========================================")
	fmt.Println()

	// 1. 查询所有用户组
	fmt.Println("1. 查询所有用户组")
	fmt.Println("----------------------------------------")
	
	cursor, err := db.Collection("cloud_iam_groups").Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("查询用户组失�? %v", err)
	}
	defer cursor.Close(ctx)

	var groups []bson.M
	if err := cursor.All(ctx, &groups); err != nil {
		log.Fatalf("解析用户组失�? %v", err)
	}

	fmt.Printf("找到 %d 个用户组\n\n", len(groups))

	// 2. 对每个用户组，修复成员数�?
	fmt.Println("2. 修复用户组成员数�?)
	fmt.Println("----------------------------------------")
	
	totalFixed := 0
	
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
		
		oldUserCount := 0
		if count, ok := group["user_count"].(int32); ok {
			oldUserCount = int(count)
		} else if count, ok := group["user_count"].(int64); ok {
			oldUserCount = int(count)
		}
		
		// 查询实际成员数量
		filter := bson.M{
			"permission_groups": groupID,
		}
		
		actualCount, err := db.Collection("cloud_iam_users").CountDocuments(ctx, filter)
		if err != nil {
			fmt.Printf("  %d. �?用户�?%s (ID: %d) 查询失败: %v\n", i+1, groupName, groupID, err)
			continue
		}
		
		if int(actualCount) != oldUserCount {
			// 更新用户组的成员数量
			update := bson.M{
				"$set": bson.M{
					"user_count": actualCount,
				},
			}
			
			_, err := db.Collection("cloud_iam_groups").UpdateOne(
				ctx,
				bson.M{"id": groupID},
				update,
			)
			
			if err != nil {
				fmt.Printf("  %d. �?用户�?%s (ID: %d) 更新失败: %v\n", i+1, groupName, groupID, err)
			} else {
				fmt.Printf("  %d. �?用户�?%s (ID: %d) 成员数量: %d -> %d\n", 
					i+1, groupName, groupID, oldUserCount, actualCount)
				totalFixed++
			}
		} else {
			fmt.Printf("  %d. �? 用户�?%s (ID: %d) 成员数量正确: %d\n", 
				i+1, groupName, groupID, oldUserCount)
		}
	}
	
	fmt.Println()
	fmt.Printf("修复完成: 共修�?%d 个用户组\n\n", totalFixed)

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

	usersWithoutGroups := 0
	usersFixed := 0
	
	for i, user := range users {
		username := "unknown"
		if u, ok := user["username"].(string); ok {
			username = u
		}
		
		userID := int64(0)
		if id, ok := user["id"].(int64); ok {
			userID = id
		} else if id, ok := user["id"].(int32); ok {
			userID = int64(id)
		}
		
		permissionGroups, hasField := user["permission_groups"]
		
		needFix := false
		if !hasField {
			needFix = true
		} else if groups, ok := permissionGroups.(bson.A); ok {
			if len(groups) == 0 {
				needFix = true
			}
		}
		
		if needFix {
			usersWithoutGroups++
			
			// 初始化为空数�?
			update := bson.M{
				"$set": bson.M{
					"permission_groups": bson.A{},
				},
			}
			
			_, err := db.Collection("cloud_iam_users").UpdateOne(
				ctx,
				bson.M{"id": userID},
				update,
			)
			
			if err != nil {
				fmt.Printf("  %d. �?用户 %s (ID: %d) 修复失败: %v\n", i+1, username, userID, err)
			} else {
				fmt.Printf("  %d. �?用户 %s (ID: %d) 已初始化 permission_groups 为空数组\n", i+1, username, userID)
				usersFixed++
			}
		}
	}
	
	if usersWithoutGroups > 0 {
		fmt.Println()
		fmt.Printf("修复完成: 共修�?%d 个用户\n", usersFixed)
		fmt.Println()
		fmt.Println("⚠️  注意: 这些用户�?permission_groups 已初始化为空数组")
		fmt.Println("   如果这些用户应该属于某些用户组，请重新同步用户组")
	} else {
		fmt.Println()
		fmt.Println("�?所有用户的 permission_groups 字段都正�?)
	}
	
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("修复完成")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("建议操作:")
	fmt.Println("  1. 运行诊断脚本验证: go run scripts/diagnose_group_members.go")
	fmt.Println("  2. 重新同步用户组以确保数据一致�?)
	fmt.Println("  3. 测试用户组成员查询功�?)
}
