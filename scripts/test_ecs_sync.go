package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// 初始化日志
	logger := elog.DefaultLogger

	// 连接 MongoDB
	ctx := context.Background()
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("连接MongoDB失败:", err)
	}
	defer client.Disconnect(ctx)

	// 创建 MongoDB 包装器
	db := &mongox.Mongo{
		Database: client.Database("ecam"),
	}

	// 初始化 DAO
	assetDAO := dao.NewAssetDAO(db)
	accountDAO := dao.NewCloudAccountDAO(db)

	// 初始化 Repository
	assetRepo := repository.NewAssetRepository(assetDAO)
	accountRepo := repository.NewCloudAccountRepository(accountDAO)

	// 初始化适配器工厂
	adapterFactory := adapters.NewAdapterFactory(logger)

	// 初始化服务
	svc := service.NewService(assetRepo, accountRepo, adapterFactory, logger)

	// 测试场景
	fmt.Println("=== 测试阿里云 ECS 同步功能 ===\n")

	// 1. 创建测试云账号
	fmt.Println("1. 创建测试云账号...")
	testAccount := shareddomain.CloudAccount{
		Name:            "测试阿里云账号",
		Provider:        shareddomain.CloudProviderAliyun,
		Environment:     shareddomain.EnvironmentDevelopment,
		AccessKeyID:     os.Getenv("ALIYUN_ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("ALIYUN_ACCESS_KEY_SECRET"),
		Region:          "cn-shenzhen",
		Description:     "用于测试ECS同步的账号",
		Status:          shareddomain.CloudAccountStatusActive,
		Config: shareddomain.CloudAccountConfig{
			EnableAutoSync:      true,
			SyncInterval:        3600,
			SupportedRegions:    []string{"cn-shenzhen", "cn-beijing"},
			SupportedAssetTypes: []string{"ecs"},
		},
		TenantID:   "test-tenant",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	accountID, err := accountRepo.Create(ctx, testAccount)
	if err != nil {
		log.Printf("创建云账号失败: %v (可能已存在)\n", err)
		// 尝试获取已存在的账号
		existingAccount, err := accountRepo.GetByName(ctx, testAccount.Name, testAccount.TenantID)
		if err != nil {
			log.Fatal("获取已存在账号失败:", err)
		}
		accountID = existingAccount.ID
		fmt.Printf("使用已存在的账号 ID: %d\n\n", accountID)
	} else {
		fmt.Printf("✓ 云账号创建成功，ID: %d\n\n", accountID)
	}

	// 2. 测试发现资产（不保存）
	fmt.Println("2. 测试发现资产（不保存到数据库）...")
	region := "cn-shenzhen"
	assetTypes := []string{"ecs"} // 指定要发现的资源类型
	assets, err := svc.DiscoverAssets(ctx, "aliyun", region, assetTypes)
	if err != nil {
		log.Fatal("发现资产失败:", err)
	}
	fmt.Printf("✓ 发现 %d 个资产（类型: %v）\n", len(assets), assetTypes)

	if len(assets) > 0 {
		fmt.Println("\n前3个实例示例:")
		for i, asset := range assets {
			if i >= 3 {
				break
			}
			fmt.Printf("  - 实例 %d:\n", i+1)
			fmt.Printf("    ID: %s\n", asset.AssetId)
			fmt.Printf("    名称: %s\n", asset.AssetName)
			fmt.Printf("    状态: %s\n", asset.Status)
			fmt.Printf("    地域: %s\n", asset.Region)
			fmt.Printf("    可用区: %s\n", asset.Zone)

			// 解析元数据显示更多信息
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(asset.Metadata), &metadata); err == nil {
				if instanceType, ok := metadata["instance_type"].(string); ok {
					fmt.Printf("    实例规格: %s\n", instanceType)
				}
				if cpu, ok := metadata["cpu"].(float64); ok {
					fmt.Printf("    CPU: %.0f 核\n", cpu)
				}
				if memory, ok := metadata["memory"].(float64); ok {
					fmt.Printf("    内存: %.0f MB\n", memory)
				}
			}
			fmt.Println()
		}
	}

	// 3. 测试同步资产（保存到数据库）
	fmt.Println("\n3. 测试同步资产到数据库...")
	// 可以指定要同步的资源类型，或传 nil/空数组同步所有支持的类型
	syncAssetTypes := []string{"ecs"}
	err = svc.SyncAssets(ctx, "aliyun", syncAssetTypes)
	if err != nil {
		log.Fatal("同步资产失败:", err)
	}
	fmt.Printf("✓ 资产同步完成（类型: %v）\n", syncAssetTypes)

	// 4. 查询已同步的资产
	fmt.Println("\n4. 查询已同步的资产...")
	filter := domain.AssetFilter{
		Provider:  "aliyun",
		AssetType: "ecs",
		Region:    region,
		Limit:     10,
	}

	syncedAssets, total, err := svc.ListAssets(ctx, filter)
	if err != nil {
		log.Fatal("查询资产失败:", err)
	}

	fmt.Printf("✓ 查询到 %d 个已同步的 ECS 实例（总共 %d 个）\n", len(syncedAssets), total)

	if len(syncedAssets) > 0 {
		fmt.Println("\n已同步的实例:")
		for i, asset := range syncedAssets {
			if i >= 5 {
				break
			}
			fmt.Printf("  %d. %s (%s) - %s\n",
				i+1,
				asset.AssetName,
				asset.AssetId,
				asset.Status)
		}
	}

	// 5. 获取资产统计
	fmt.Println("\n5. 获取资产统计...")
	stats, err := svc.GetAssetStatistics(ctx)
	if err != nil {
		log.Fatal("获取统计失败:", err)
	}

	fmt.Printf("✓ 资产统计:\n")
	fmt.Printf("  总资产数: %d\n", stats.TotalAssets)
	fmt.Printf("  按云厂商统计: %v\n", stats.ProviderStats)
	fmt.Printf("  按资产类型统计: %v\n", stats.AssetTypeStats)
	fmt.Printf("  按地域统计: %v\n", stats.RegionStats)
	fmt.Printf("  按状态统计: %v\n", stats.StatusStats)

	fmt.Println("\n=== 测试完成 ===")
}
