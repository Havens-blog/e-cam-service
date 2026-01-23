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
	fmt.Println("用户组成员诊�?)
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

	// 2. 查询所有用�?
	fmt.Println("2. 查询所有用�?)
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

	// 3. 检查每个用户的 permission_groups 字段
	fmt.Println("3. 检查用户的 permission_groups 字段")
	fmt.Println("----------------------------------------")
	
	usersWithGroups := 0
	usersWithoutGroups := 0
	
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
		
		if !hasField {
			fmt.Printf("  %d. �?用户 %s (ID: %d) 没有 permission_groups 字段\n", i+1, username, userID)
			usersWithoutGroups++
		} else {
			if groups, ok := permissionGroups.(bson.A); ok {
				if len(groups) > 0 {
					fmt.Printf("  %d. �?用户 %s (ID: %d) �?%d 个用户组: %v\n", i+1, username, userID, len(groups), groups)
					usersWithGroups++
				} else {
					fmt.Printf("  %d. ⚠️  用户 %s (ID: %d) permission_groups 为空数组\n", i+1, username, userID)
					usersWithoutGroups++
				}
			} else {
				fmt.Printf("  %d. ⚠️  用户 %s (ID: %d) permission_groups 类型错误: %T\n", i+1, username, userID, permissionGroups)
				usersWithoutGroups++
			}
		}
	}
	
	fmt.Println()
	fmt.Printf("统计: 有用户组的用�?%d �? 无用户组的用�?%d 个\n\n", usersWithGroups, usersWithoutGroups)

	// 4. 对每个用户组，查询其成员
	fmt.Println("4. 查询每个用户组的成员")
	fmt.Println("----------------------------------------")
	
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
		
		userCount := 0
		if count, ok := group["user_count"].(int32); ok {
			userCount = int(count)
		} else if count, ok := group["user_count"].(int64); ok {
			userCount = int(count)
		}
		
		// 查询包含该用户组的用�?
		filter := bson.M{
			"permission_groups": groupID,
		}
		
		cursor3, err := db.Collection("cloud_iam_users").Find(ctx, filter)
		if err != nil {
			fmt.Printf("  %d. �?用户�?%s (ID: %d) 查询失败: %v\n", i+1, groupName, groupID, err)
			continue
		}
		
		var members []bson.M
		if err := cursor3.All(ctx, &members); err != nil {
			fmt.Printf("  %d. �?用户�?%s (ID: %d) 解析失败: %v\n", i+1, groupName, groupID, err)
			cursor3.Close(ctx)
			continue
		}
		cursor3.Close(ctx)
		
		if len(members) != userCount {
			fmt.Printf("  %d. ⚠️  用户�?%s (ID: %d) user_count=%d, 实际查询�?%d 个成员\n", 
				i+1, groupName, groupID, userCount, len(members))
		} else {
			fmt.Printf("  %d. �?用户�?%s (ID: %d) user_count=%d, 查询�?%d 个成员\n", 
				i+1, groupName, groupID, userCount, len(members))
		}
		
		// 显示成员列表
		if len(members) > 0 {
			fmt.Printf("     成员列表:\n")
			for j, member := range members {
				username := "unknown"
				if u, ok := member["username"].(string); ok {
					username = u
				}
				fmt.Printf("       %d. %s\n", j+1, username)
			}
		}
		fmt.Println()
	}

	// 5. 问题诊断
	fmt.Println("5. 问题诊断")
	fmt.Println("----------------------------------------")
	
	if usersWithoutGroups > 0 {
		fmt.Println("�?发现问题：有用户�?permission_groups 字段为空或不存在")
		fmt.Println()
		fmt.Println("可能原因�?)
		fmt.Println("  1. 用户同步时没有正确设�?permission_groups")
		fmt.Println("  2. 用户是手动创建的，没有分配用户组")
		fmt.Println("  3. 数据迁移时字段丢�?)
		fmt.Println()
		fmt.Println("解决方案�?)
		fmt.Println("  1. 重新同步用户组（会自动同步成员关系）")
		fmt.Println("  2. 手动为用户分配用户组")
		fmt.Println("  3. 运行数据修复脚本")
	} else {
		fmt.Println("�?所有用户都�?permission_groups 字段")
	}
	
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("诊断完成")
	fmt.Println("========================================")
}
