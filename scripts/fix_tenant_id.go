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
	// 从环境变量获取 MongoDB 连接信息
	mongoURI := getEnv("MONGO_URI", "mongodb://admin:password@localhost:27017")
	database := getEnv("MONGO_DATABASE", "e_cam_service")

	fmt.Println("=== 修复 Tenant ID 问题 ===")
	fmt.Printf("MongoDB URI: %s\n", mongoURI)
	fmt.Printf("Database: %s\n\n", database)

	// 连接 MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("连接 MongoDB 失败: %v", err)
	}
	defer client.Disconnect(ctx)

	// 测试连接
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Ping MongoDB 失败: %v", err)
	}
	fmt.Println("✓ MongoDB 连接成功\n")

	db := client.Database(database)

	// 1. 检查租户集合
	fmt.Println("步骤 1: 检查租户集合...")
	tenantsCollection := db.Collection("tenants")
	
	totalTenants, err := tenantsCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计租户数量失败: %v", err)
	}
	fmt.Printf("  总租户数: %d\n", totalTenants)

	if totalTenants == 0 {
		fmt.Println("  ⚠️  警告: 没有找到任何租户数据")
		fmt.Println("\n建议: 先创建租户")
		return
	}

	// 查看租户列表
	cursor, err := tenantsCollection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("查询租户失败: %v", err)
	}
	defer cursor.Close(ctx)

	type Tenant struct {
		ID   string `bson:"_id"`
		Name string `bson:"name"`
	}

	var tenants []Tenant
	if err := cursor.All(ctx, &tenants); err != nil {
		log.Fatalf("读取租户失败: %v", err)
	}

	fmt.Println("  租户列表:")
	for i, tenant := range tenants {
		fmt.Printf("    %d. ID: %s, 名称: %s\n", i+1, tenant.ID, tenant.Name)
	}

	// 2. 检查云账号的 tenant_id
	fmt.Println("\n步骤 2: 检查云账号的 tenant_id...")
	accountsCollection := db.Collection("cloud_accounts")
	
	totalAccounts, err := accountsCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计云账号数量失败: %v", err)
	}
	fmt.Printf("  总云账号数: %d\n", totalAccounts)

	if totalAccounts == 0 {
		fmt.Println("  ⚠️  警告: 没有找到任何云账号")
		return
	}

	cursor, err = accountsCollection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("查询云账号失败: %v", err)
	}
	defer cursor.Close(ctx)

	type Account struct {
		ID       int64  `bson:"id"`
		Name     string `bson:"name"`
		TenantID string `bson:"tenant_id"`
	}

	var accounts []Account
	if err := cursor.All(ctx, &accounts); err != nil {
		log.Fatalf("读取云账号失败: %v", err)
	}

	fmt.Println("  云账号列表:")
	invalidAccounts := []Account{}
	for i, account := range accounts {
		fmt.Printf("    %d. ID: %d, 名称: %s, TenantID: %s", i+1, account.ID, account.Name, account.TenantID)
		
		// 检查 tenant_id 是否有效
		validTenant := false
		for _, tenant := range tenants {
			if tenant.ID == account.TenantID {
				validTenant = true
				break
			}
		}
		
		if !validTenant {
			fmt.Printf(" ❌ (无效)\n")
			invalidAccounts = append(invalidAccounts, account)
		} else {
			fmt.Printf(" ✓\n")
		}
	}

	// 3. 修复无效的 tenant_id
	if len(invalidAccounts) > 0 {
		fmt.Printf("\n步骤 3: 修复 %d 个无效的 tenant_id...\n", len(invalidAccounts))
		
		if len(tenants) == 0 {
			fmt.Println("  ⚠️  没有可用的租户，无法修复")
			return
		}

		// 使用第一个租户作为默认租户
		defaultTenant := tenants[0]
		fmt.Printf("  使用默认租户: %s (%s)\n", defaultTenant.ID, defaultTenant.Name)

		for _, account := range invalidAccounts {
			filter := bson.M{"id": account.ID}
			update := bson.M{"$set": bson.M{"tenant_id": defaultTenant.ID}}
			
			result, err := accountsCollection.UpdateOne(ctx, filter, update)
			if err != nil {
				fmt.Printf("  ✗ 更新云账号 %d 失败: %v\n", account.ID, err)
			} else if result.ModifiedCount > 0 {
				fmt.Printf("  ✓ 更新云账号 %d 的 tenant_id: %s -> %s\n", 
					account.ID, account.TenantID, defaultTenant.ID)
			}
		}
	} else {
		fmt.Println("\n步骤 3: 所有云账号的 tenant_id 都有效 ✓")
	}

	// 4. 检查用户的 tenant_id
	fmt.Println("\n步骤 4: 检查用户的 tenant_id...")
	usersCollection := db.Collection("cloud_iam_users")
	
	totalUsers, err := usersCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计用户数量失败: %v", err)
	}
	fmt.Printf("  总用户数: %d\n", totalUsers)

	if totalUsers == 0 {
		fmt.Println("  ⚠️  没有用户数据")
		fmt.Println("\n=== 修复完成 ===")
		return
	}

	// 统计各个 tenant_id 的用户数
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.M{
			"_id":   "$tenant_id",
			"count": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.M{"count": -1}}},
	}

	cursor, err = usersCollection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Fatalf("聚合查询失败: %v", err)
	}
	defer cursor.Close(ctx)

	type TenantStat struct {
		TenantID string `bson:"_id"`
		Count    int    `bson:"count"`
	}

	var tenantStats []TenantStat
	if err := cursor.All(ctx, &tenantStats); err != nil {
		log.Fatalf("读取聚合结果失败: %v", err)
	}

	fmt.Println("  用户按 tenant_id 分布:")
	invalidUserCount := 0
	for i, stat := range tenantStats {
		tenantID := stat.TenantID
		if tenantID == "" {
			tenantID = "<空>"
		}
		
		// 检查是否有效
		validTenant := false
		for _, tenant := range tenants {
			if tenant.ID == stat.TenantID {
				validTenant = true
				break
			}
		}
		
		if validTenant {
			fmt.Printf("    %d. TenantID: %s - 用户数: %d ✓\n", i+1, tenantID, stat.Count)
		} else {
			fmt.Printf("    %d. TenantID: %s - 用户数: %d ❌ (无效)\n", i+1, tenantID, stat.Count)
			invalidUserCount += stat.Count
		}
	}

	// 5. 修复用户的 tenant_id
	if invalidUserCount > 0 {
		fmt.Printf("\n步骤 5: 修复 %d 个用户的 tenant_id...\n", invalidUserCount)
		
		if len(tenants) == 0 {
			fmt.Println("  ⚠️  没有可用的租户，无法修复")
			return
		}

		defaultTenant := tenants[0]
		fmt.Printf("  使用默认租户: %s (%s)\n", defaultTenant.ID, defaultTenant.Name)

		// 构建无效 tenant_id 的查询条件
		validTenantIDs := make([]string, len(tenants))
		for i, tenant := range tenants {
			validTenantIDs[i] = tenant.ID
		}

		filter := bson.M{
			"tenant_id": bson.M{"$nin": validTenantIDs},
		}
		update := bson.M{"$set": bson.M{"tenant_id": defaultTenant.ID}}
		
		result, err := usersCollection.UpdateMany(ctx, filter, update)
		if err != nil {
			fmt.Printf("  ✗ 批量更新用户失败: %v\n", err)
		} else {
			fmt.Printf("  ✓ 成功更新 %d 个用户的 tenant_id\n", result.ModifiedCount)
		}
	} else {
		fmt.Println("\n步骤 5: 所有用户的 tenant_id 都有效 ✓")
	}

	// 6. 检查用户组的 tenant_id
	fmt.Println("\n步骤 6: 检查用户组的 tenant_id...")
	groupsCollection := db.Collection("cloud_iam_groups")
	
	totalGroups, err := groupsCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计用户组数量失败: %v", err)
	}
	fmt.Printf("  总用户组数: %d\n", totalGroups)

	if totalGroups > 0 {
		// 统计各个 tenant_id 的用户组数
		cursor, err = groupsCollection.Aggregate(ctx, pipeline)
		if err != nil {
			log.Fatalf("聚合查询失败: %v", err)
		}
		defer cursor.Close(ctx)

		var groupTenantStats []TenantStat
		if err := cursor.All(ctx, &groupTenantStats); err != nil {
			log.Fatalf("读取聚合结果失败: %v", err)
		}

		fmt.Println("  用户组按 tenant_id 分布:")
		invalidGroupCount := 0
		for i, stat := range groupTenantStats {
			tenantID := stat.TenantID
			if tenantID == "" {
				tenantID = "<空>"
			}
			
			validTenant := false
			for _, tenant := range tenants {
				if tenant.ID == stat.TenantID {
					validTenant = true
					break
				}
			}
			
			if validTenant {
				fmt.Printf("    %d. TenantID: %s - 用户组数: %d ✓\n", i+1, tenantID, stat.Count)
			} else {
				fmt.Printf("    %d. TenantID: %s - 用户组数: %d ❌ (无效)\n", i+1, tenantID, stat.Count)
				invalidGroupCount += stat.Count
			}
		}

		// 修复用户组的 tenant_id
		if invalidGroupCount > 0 {
			fmt.Printf("\n  修复 %d 个用户组的 tenant_id...\n", invalidGroupCount)
			
			defaultTenant := tenants[0]
			validTenantIDs := make([]string, len(tenants))
			for i, tenant := range tenants {
				validTenantIDs[i] = tenant.ID
			}

			filter := bson.M{
				"tenant_id": bson.M{"$nin": validTenantIDs},
			}
			update := bson.M{"$set": bson.M{"tenant_id": defaultTenant.ID}}
			
			result, err := groupsCollection.UpdateMany(ctx, filter, update)
			if err != nil {
				fmt.Printf("  ✗ 批量更新用户组失败: %v\n", err)
			} else {
				fmt.Printf("  ✓ 成功更新 %d 个用户组的 tenant_id\n", result.ModifiedCount)
			}
		}
	}

	fmt.Println("\n=== 修复完成 ===")
	fmt.Println("\n建议:")
	fmt.Println("  1. 重新查询用户列表，应该能看到数据了")
	fmt.Println("  2. 如果还有问题，检查请求头中的 X-Tenant-ID 是否正确")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
