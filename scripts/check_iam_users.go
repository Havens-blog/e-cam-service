//go:build ignore
// +build ignore

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
	// 从环境变量获�?MongoDB 连接信息
	mongoURI := getEnv("MONGO_URI", "mongodb://admin:password@localhost:27017")
	database := getEnv("MONGO_DATABASE", "e_cam_service")

	fmt.Println("=== 检�?IAM 用户数据 ===")
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
	fmt.Println("�?MongoDB 连接成功\n")

	db := client.Database(database)

	// 1. 检查用户集�?
	fmt.Println("步骤 1: 检查用户集�?..")
	usersCollection := db.Collection("cloud_iam_users")
	
	totalUsers, err := usersCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计用户数量失败: %v", err)
	}
	fmt.Printf("  总用户数: %d\n", totalUsers)

	if totalUsers == 0 {
		fmt.Println("  ⚠️  警告: 没有找到任何用户数据")
		fmt.Println("\n可能的原�?")
		fmt.Println("  1. 还没有执行过用户同步")
		fmt.Println("  2. 集合名称不正�?)
		fmt.Println("  3. 数据库名称不正确")
		fmt.Println("\n建议:")
		fmt.Println("  执行用户组同�? POST /api/v1/cam/iam/groups/sync?cloud_account_id=<id>")
		return
	}

	// 2. 按租户统�?
	fmt.Println("\n步骤 2: 按租户统计用�?..")
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.M{
			"_id":   "$tenant_id",
			"count": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.M{"count": -1}}},
	}

	cursor, err := usersCollection.Aggregate(ctx, pipeline)
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

	if len(tenantStats) == 0 {
		fmt.Println("  ⚠️  没有租户数据")
	} else {
		for i, stat := range tenantStats {
			tenantID := stat.TenantID
			if tenantID == "" {
				tenantID = "<�?"
			}
			fmt.Printf("  %d. 租户ID: %s - 用户�? %d\n", i+1, tenantID, stat.Count)
		}
	}

	// 3. 按云厂商统计
	fmt.Println("\n步骤 3: 按云厂商统计用户...")
	pipeline = mongo.Pipeline{
		{{Key: "$group", Value: bson.M{
			"_id":   "$provider",
			"count": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.M{"count": -1}}},
	}

	cursor, err = usersCollection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Fatalf("聚合查询失败: %v", err)
	}
	defer cursor.Close(ctx)

	type ProviderStat struct {
		Provider string `bson:"_id"`
		Count    int    `bson:"count"`
	}

	var providerStats []ProviderStat
	if err := cursor.All(ctx, &providerStats); err != nil {
		log.Fatalf("读取聚合结果失败: %v", err)
	}

	for i, stat := range providerStats {
		provider := stat.Provider
		if provider == "" {
			provider = "<�?"
		}
		fmt.Printf("  %d. 云厂�? %s - 用户�? %d\n", i+1, provider, stat.Count)
	}

	// 4. 查看示例用户
	fmt.Println("\n步骤 4: 查看示例用户（前5个）...")
	cursor, err = usersCollection.Find(ctx, bson.M{}, options.Find().SetLimit(5).SetSort(bson.M{"ctime": -1}))
	if err != nil {
		log.Fatalf("查询用户失败: %v", err)
	}
	defer cursor.Close(ctx)

	type User struct {
		ID         int64  `bson:"id"`
		Username   string `bson:"username"`
		Provider   string `bson:"provider"`
		TenantID   string `bson:"tenant_id"`
		UserGroups []int64 `bson:"permission_groups"`
	}

	var users []User
	if err := cursor.All(ctx, &users); err != nil {
		log.Fatalf("读取用户失败: %v", err)
	}

	for i, user := range users {
		fmt.Printf("  %d. ID: %d, 用户�? %s, 云厂�? %s, 租户: %s, 用户�? %v\n",
			i+1, user.ID, user.Username, user.Provider, user.TenantID, user.UserGroups)
	}

	// 5. 检查用户组集合
	fmt.Println("\n步骤 5: 检查用户组集合...")
	groupsCollection := db.Collection("cloud_iam_groups")
	
	totalGroups, err := groupsCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("统计用户组数量失�? %v", err)
	}
	fmt.Printf("  总用户组�? %d\n", totalGroups)

	if totalGroups > 0 {
		// 查看示例用户�?
		cursor, err = groupsCollection.Find(ctx, bson.M{}, options.Find().SetLimit(3))
		if err != nil {
			log.Fatalf("查询用户组失�? %v", err)
		}
		defer cursor.Close(ctx)

		type Group struct {
			ID          int64  `bson:"id"`
			Name        string `bson:"name"`
			GroupName   string `bson:"group_name"`
			TenantID    string `bson:"tenant_id"`
			MemberCount int    `bson:"member_count"`
		}

		var groups []Group
		if err := cursor.All(ctx, &groups); err != nil {
			log.Fatalf("读取用户组失�? %v", err)
		}

		fmt.Println("  示例用户组（�?个）:")
		for i, group := range groups {
			fmt.Printf("    %d. ID: %d, 名称: %s, 租户: %s, 成员�? %d\n",
				i+1, group.ID, group.Name, group.TenantID, group.MemberCount)
		}
	}

	// 6. 测试查询
	fmt.Println("\n步骤 6: 测试查询条件...")
	
	// 测试不同的查询条�?
	testQueries := []struct {
		name  string
		query bson.M
	}{
		{"无条件查�?, bson.M{}},
		{"按租户查询（tenant-001�?, bson.M{"tenant_id": "tenant-001"}},
		{"按云厂商查询（aliyun�?, bson.M{"provider": "aliyun"}},
		{"按租户和云厂商查�?, bson.M{"tenant_id": "tenant-001", "provider": "aliyun"}},
	}

	for _, test := range testQueries {
		count, err := usersCollection.CountDocuments(ctx, test.query)
		if err != nil {
			fmt.Printf("  �?%s: 查询失败 - %v\n", test.name, err)
		} else {
			fmt.Printf("  �?%s: %d 条记录\n", test.name, count)
		}
	}

	fmt.Println("\n=== 检查完�?===")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
