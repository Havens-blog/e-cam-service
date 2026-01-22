//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camdao "github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	syncdomain "github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	cmdbdomain "github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
	cmdbrepository "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	cmdbdao "github.com/Havens-blog/e-cam-service/internal/cmdb/repository/dao"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	tenantID = "default"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 连接 MongoDB
	credential := options.Credential{
		Username:   "ecmdb",
		Password:   "123456",
		AuthSource: "admin",
	}
	clientOpts := options.Client().
		ApplyURI("mongodb://106.52.187.69:27017").
		SetAuth(credential)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatal("连接MongoDB失败:", err)
	}
	defer client.Disconnect(ctx)

	mongoDB := mongox.NewMongo(client, "ecmdb")

	// 创建 logger
	logger := createLogger()

	// 初始化 DAO 和 Repository
	accountDAO := camdao.NewCloudAccountDAO(mongoDB)
	accountRepo := camrepo.NewCloudAccountRepository(accountDAO)

	instanceDAO := cmdbdao.NewInstanceDAO(mongoDB)
	instanceRepo := cmdbrepository.NewInstanceRepository(instanceDAO)

	// 获取云账号
	fmt.Println("=== 获取云账号 ===")
	filter := shareddomain.CloudAccountFilter{
		Provider: shareddomain.CloudProviderAliyun,
		Status:   shareddomain.CloudAccountStatusActive,
		Limit:    10,
	}
	accounts, total, err := accountRepo.List(ctx, filter)
	if err != nil {
		log.Fatal("获取云账号失败:", err)
	}
	fmt.Printf("找到 %d 个阿里云账号\n", total)

	if len(accounts) == 0 {
		log.Fatal("没有可用的阿里云账号，请先添加云账号")
	}

	account := accounts[0]
	fmt.Printf("使用账号: %s (ID: %d)\n", account.Name, account.ID)

	// 创建适配器
	defaultRegion := "cn-hangzhou"
	if len(account.Regions) > 0 {
		defaultRegion = account.Regions[0]
	}

	syncAccount := &syncdomain.CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        syncdomain.CloudProvider(account.Provider),
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret,
		DefaultRegion:   defaultRegion,
		Enabled:         true,
	}

	adapterFactory := adapters.NewAdapterFactory(logger)
	adapter, err := adapterFactory.CreateAdapter(syncAccount)
	if err != nil {
		log.Fatal("创建适配器失败:", err)
	}

	// 获取地域列表
	fmt.Println("\n=== 获取地域列表 ===")
	regions, err := adapter.GetRegions(ctx)
	if err != nil {
		log.Fatal("获取地域失败:", err)
	}
	fmt.Printf("共 %d 个地域\n", len(regions))

	// 只同步前10个地域（测试用）
	testRegions := regions
	if len(testRegions) > 10 {
		testRegions = testRegions[:10]
	}

	// 同步 ECS 实例
	fmt.Println("\n=== 同步 ECS 实例到 c_instance ===")
	totalSynced := 0

	for _, region := range testRegions {
		fmt.Printf("\n地域: %s (%s)\n", region.LocalName, region.ID)

		instances, err := adapter.GetECSInstances(ctx, region.ID)
		if err != nil {
			fmt.Printf("  获取ECS失败: %v\n", err)
			continue
		}

		if len(instances) == 0 {
			fmt.Println("  无实例")
			continue
		}

		fmt.Printf("  发现 %d 个实例\n", len(instances))

		// 转换并保存到 c_instance
		for _, inst := range instances {
			cmdbInstance := convertToCMDBInstance(tenantID, &account, inst)

			err := instanceRepo.Upsert(ctx, cmdbInstance)
			if err != nil {
				fmt.Printf("  保存失败 %s: %v\n", inst.InstanceID, err)
				continue
			}
			fmt.Printf("  ✓ %s (%s) - %s\n", inst.InstanceID, inst.InstanceName, inst.Status)
			totalSynced++
		}
	}

	fmt.Printf("\n=== 同步完成 ===\n")
	fmt.Printf("共同步 %d 个实例到 c_instance (model_uid: cloud_vm)\n", totalSynced)

	// 验证数据
	fmt.Println("\n=== 验证数据 ===")
	cmdbFilter := cmdbdomain.InstanceFilter{
		ModelUID: "cloud_vm",
		TenantID: tenantID,
		Limit:    10,
	}
	savedInstances, err := instanceRepo.List(ctx, cmdbFilter)
	if err != nil {
		log.Fatal("查询失败:", err)
	}
	fmt.Printf("c_instance 中 cloud_vm 实例数: %d\n", len(savedInstances))

	for _, inst := range savedInstances {
		fmt.Printf("  - %s: %s (account_id: %d)\n", inst.AssetID, inst.AssetName, inst.AccountID)
	}
}

// convertToCMDBInstance 将 ECS 实例转换为 CMDB Instance
func convertToCMDBInstance(tenantID string, account *shareddomain.CloudAccount, inst syncdomain.ECSInstance) cmdbdomain.Instance {
	attrs := map[string]interface{}{
		// 基本信息
		"cloud_id":         inst.InstanceID,
		"instance_id":      inst.InstanceID,
		"instance_name":    inst.InstanceName,
		"status":           inst.Status,
		"platform":         inst.Provider,
		"region":           inst.Region,
		"zone":             inst.Zone,
		"cloud_account_id": account.ID,
		"creation_time":    inst.CreationTime,
		"tags":             inst.Tags,
		"hostname":         inst.HostName,
		"charge_type":      inst.ChargeType,

		// 配置信息
		"os_type":       inst.OSType,
		"private_ip":    inst.PrivateIP,
		"public_ip":     inst.PublicIP,
		"os_image":      inst.ImageID,
		"vpc_id":        inst.VPCID,
		"cpu":           inst.CPU,
		"memory":        inst.Memory,
		"instance_type": inst.InstanceType,

		// 网络
		"security_groups":   inst.SecurityGroups,
		"max_bandwidth_in":  inst.InternetMaxBandwidthIn,
		"max_bandwidth_out": inst.InternetMaxBandwidthOut,

		// 存储
		"system_disk_size":     inst.SystemDiskSize,
		"system_disk_category": inst.SystemDiskCategory,
	}

	return cmdbdomain.Instance{
		ModelUID:   "cloud_vm",
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   tenantID,
		AccountID:  account.ID,
		Attributes: attrs,
	}
}

// createLogger 创建一个简单的 logger
func createLogger() *elog.Component {
	// 使用 elog.DefaultLogger，如果为 nil 则创建一个
	if elog.DefaultLogger != nil {
		return elog.DefaultLogger
	}
	// 返回 nil，让 adapter 内部处理
	return nil
}
