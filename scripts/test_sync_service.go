//go:build ignore
// +build ignore

// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/gotomicro/ego/core/elog"
)

func main() {
	logger := elog.DefaultLogger

	// 从环境变量获取阿里云凭证
	accessKeyID := os.Getenv("ALIYUN_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIYUN_ACCESS_KEY_SECRET")

	if accessKeyID == "" || accessKeySecret == "" {
		fmt.Println("�?请设置环境变�?ALIYUN_ACCESS_KEY_ID �?ALIYUN_ACCESS_KEY_SECRET")
		os.Exit(1)
	}

	fmt.Println("🔌 测试云主机同步服�?)
	fmt.Println("=====================================")

	// 创建适配器工�?
	factory := adapters.NewAdapterFactory(logger)

	// 创建同步服务
	syncService := service.NewSyncService(factory, logger)

	// 创建云账号配�?
	account := &domain.CloudAccount{
		ID:              1,
		Name:            "测试阿里云账�?,
		Provider:        domain.ProviderAliyun,
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		DefaultRegion:   "cn-shenzhen",
		Enabled:         true,
		Description:     "用于测试的阿里云账号",
	}

	ctx := context.Background()

	// 测试1: 同步指定地域的云主机
	fmt.Println("\n【测�?: 同步指定地域的云主机�?)
	testRegions := []string{"cn-hangzhou"}
	
	result, err := syncService.SyncECSInstances(ctx, account, testRegions)
	if err != nil {
		fmt.Printf("�?同步失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("�?同步完成")
	fmt.Printf("  总数:       %d\n", result.TotalCount)
	fmt.Printf("  新增:       %d\n", result.AddedCount)
	fmt.Printf("  更新:       %d\n", result.UpdatedCount)
	fmt.Printf("  删除:       %d\n", result.DeletedCount)
	fmt.Printf("  未变�?     %d\n", result.UnchangedCount)
	fmt.Printf("  错误:       %d\n", result.ErrorCount)
	fmt.Printf("  耗时:       %v\n", result.Duration)
	fmt.Printf("  成功:       %v\n", result.Success)

	if len(result.Errors) > 0 {
		fmt.Println("\n  错误详情:")
		for i, err := range result.Errors {
			if i >= 5 {
				fmt.Printf("  ... 还有 %d 个错误\n", len(result.Errors)-5)
				break
			}
			fmt.Printf("    - %s: %s\n", err.ResourceID, err.Error)
		}
	}

	// 测试2: 同步所有地域（注释掉，避免耗时太长�?
	/*
	fmt.Println("\n【测�?: 同步所有地域的云主机�?)
	result2, err := syncService.SyncECSInstances(ctx, account, nil)
	if err != nil {
		fmt.Printf("�?同步失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("�?同步完成")
	fmt.Printf("  总数:       %d\n", result2.TotalCount)
	fmt.Printf("  新增:       %d\n", result2.AddedCount)
	fmt.Printf("  更新:       %d\n", result2.UpdatedCount)
	fmt.Printf("  删除:       %d\n", result2.DeletedCount)
	fmt.Printf("  未变�?     %d\n", result2.UnchangedCount)
	fmt.Printf("  错误:       %d\n", result2.ErrorCount)
	fmt.Printf("  耗时:       %v\n", result2.Duration)
	*/

	// 测试3: 检测实例变�?
	fmt.Println("\n【测�?: 检测实例变化�?)
	
	// 模拟已存在的实例
	existingInstances := make(map[string]*domain.ECSInstance)
	existingInstances["i-test-1"] = &domain.ECSInstance{
		InstanceID:   "i-test-1",
		InstanceName: "test-instance-1",
		Status:       "Running",
		PublicIP:     "1.2.3.4",
	}
	existingInstances["i-test-2"] = &domain.ECSInstance{
		InstanceID:   "i-test-2",
		InstanceName: "test-instance-2",
		Status:       "Running",
		PublicIP:     "1.2.3.5",
	}

	// 模拟新获取的实例
	newInstances := []domain.ECSInstance{
		{
			InstanceID:   "i-test-1",
			InstanceName: "test-instance-1",
			Status:       "Stopped", // 状态变�?
			PublicIP:     "1.2.3.4",
		},
		{
			InstanceID:   "i-test-2",
			InstanceName: "test-instance-2",
			Status:       "Running", // 无变�?
			PublicIP:     "1.2.3.5",
		},
		{
			InstanceID:   "i-test-3",
			InstanceName: "test-instance-3",
			Status:       "Running", // 新增
			PublicIP:     "1.2.3.6",
		},
	}
	// i-test-4 被删除了（不在新列表中）
	existingInstances["i-test-4"] = &domain.ECSInstance{
		InstanceID:   "i-test-4",
		InstanceName: "test-instance-4",
		Status:       "Running",
	}

	added, updated, deleted, unchanged := syncService.DetectInstanceChanges(existingInstances, newInstances)

	fmt.Printf("�?变化检测完成\n")
	fmt.Printf("  新增:       %d\n", len(added))
	if len(added) > 0 {
		for _, inst := range added {
			fmt.Printf("    - %s (%s)\n", inst.InstanceID, inst.InstanceName)
		}
	}
	
	fmt.Printf("  更新:       %d\n", len(updated))
	if len(updated) > 0 {
		for _, inst := range updated {
			fmt.Printf("    - %s (%s): %s\n", inst.InstanceID, inst.InstanceName, inst.Status)
		}
	}
	
	fmt.Printf("  删除:       %d\n", len(deleted))
	if len(deleted) > 0 {
		for _, inst := range deleted {
			fmt.Printf("    - %s (%s)\n", inst.InstanceID, inst.InstanceName)
		}
	}
	
	fmt.Printf("  未变�?     %d\n", len(unchanged))

	// 测试4: 创建同步任务
	fmt.Println("\n【测�?: 同步任务生命周期�?)
	task := &domain.SyncTask{
		ID:           1,
		AccountID:    account.ID,
		Provider:     account.Provider,
		ResourceType: "ecs",
		Region:       "cn-hangzhou",
		Status:       domain.TaskStatusPending,
	}

	fmt.Printf("初始状�? %s\n", task.Status)
	
	// 开始任�?
	task.Start()
	fmt.Printf("开始任�? %s (开始时�? %d)\n", task.Status, task.StartTime)
	
	// 完成任务
	task.Complete(result)
	fmt.Printf("完成任务: %s\n", task.Status)
	fmt.Printf("  总数:       %d\n", task.TotalCount)
	fmt.Printf("  新增:       %d\n", task.AddedCount)
	fmt.Printf("  更新:       %d\n", task.UpdatedCount)
	fmt.Printf("  删除:       %d\n", task.DeletedCount)
	fmt.Printf("  未变�?     %d\n", task.UnchangedCount)
	fmt.Printf("  错误:       %d\n", task.ErrorCount)
	fmt.Printf("  耗时:       %d 秒\n", task.Duration)
	fmt.Printf("  成功�?     %.2f%%\n", task.GetSuccessRate())

	fmt.Println("\n=====================================")
	fmt.Println("🎉 同步服务测试完成�?)
}
