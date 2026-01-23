//go:build ignore
// +build ignore

// +build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"time"

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

	fmt.Println("🔌 测试阿里云ECS详细信息获取")
	fmt.Println("=====================================")

	// 创建适配�?
	config := adapters.AliyunConfig{
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		DefaultRegion:   "cn-shenzhen",
	}
	adapter := adapters.NewAliyunAdapter(config, logger)

	ctx := context.Background()
	testRegion := "cn-hangzhou"

	// 1. 获取基本实例列表
	fmt.Printf("\n�?. 获取基本实例列表 - %s】\n", testRegion)
	instances, err := adapter.GetECSInstances(ctx, testRegion)
	if err != nil {
		fmt.Printf("�?获取实例列表失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("�?获取�?%d 个实例\n", len(instances))

	if len(instances) == 0 {
		fmt.Println("⚠️  没有实例，跳过详细信息测�?)
		return
	}

	testInstanceID := instances[0].InstanceID
	fmt.Printf("\n使用实例 %s (%s) 进行测试\n", testInstanceID, instances[0].InstanceName)

	// 2. 获取单个实例详情
	fmt.Println("\n�?. 获取单个实例详情�?)
	instanceDetail, err := adapter.GetECSInstanceDetail(ctx, testRegion, testInstanceID)
	if err != nil {
		fmt.Printf("�?获取实例详情失败: %v\n", err)
	} else {
		fmt.Println("�?获取实例详情成功")
		fmt.Printf("  实例ID:       %s\n", instanceDetail.InstanceID)
		fmt.Printf("  实例名称:     %s\n", instanceDetail.InstanceName)
		fmt.Printf("  状�?         %s\n", instanceDetail.Status)
		fmt.Printf("  实例规格:     %s\n", instanceDetail.InstanceType)
		fmt.Printf("  规格�?       %s\n", instanceDetail.InstanceTypeFamily)
		fmt.Printf("  CPU:          %d 核\n", instanceDetail.CPU)
		fmt.Printf("  内存:         %d MB\n", instanceDetail.Memory)
		fmt.Printf("  镜像ID:       %s\n", instanceDetail.ImageID)
		fmt.Printf("  主机�?       %s\n", instanceDetail.HostName)
		fmt.Printf("  密钥�?       %s\n", instanceDetail.KeyPairName)
		fmt.Printf("  公网IP:       %s\n", instanceDetail.PublicIP)
		fmt.Printf("  私网IP:       %s\n", instanceDetail.PrivateIP)
		fmt.Printf("  VPC ID:       %s\n", instanceDetail.VPCID)
		fmt.Printf("  交换机ID:     %s\n", instanceDetail.VSwitchID)
		fmt.Printf("  安全�?       %v\n", instanceDetail.SecurityGroups)
		fmt.Printf("  公网入带�?   %d Mbps\n", instanceDetail.InternetMaxBandwidthIn)
		fmt.Printf("  公网出带�?   %d Mbps\n", instanceDetail.InternetMaxBandwidthOut)
		fmt.Printf("  I/O优化:      %s\n", instanceDetail.IoOptimized)
		fmt.Printf("  网络类型:     %s\n", instanceDetail.NetworkType)
		fmt.Printf("  计费方式:     %s\n", instanceDetail.ChargeType)
		fmt.Printf("  创建时间:     %s\n", instanceDetail.CreationTime)
		if len(instanceDetail.Tags) > 0 {
			fmt.Printf("  标签:         %v\n", instanceDetail.Tags)
		}
	}

	// 3. 获取实例磁盘信息
	fmt.Println("\n�?. 获取实例磁盘信息�?)
	disks, err := adapter.GetInstanceDisks(ctx, testRegion, testInstanceID)
	if err != nil {
		fmt.Printf("�?获取磁盘信息失败: %v\n", err)
	} else {
		fmt.Printf("�?获取�?%d 个数据盘\n", len(disks))
		for i, disk := range disks {
			fmt.Printf("\n  数据�?%d:\n", i+1)
			fmt.Printf("    磁盘ID:   %s\n", disk.DiskID)
			fmt.Printf("    类型:     %s\n", disk.Category)
			fmt.Printf("    大小:     %d GB\n", disk.Size)
			fmt.Printf("    设备�?   %s\n", disk.Device)
		}
	}

	// 4. 获取实例列表（含详细信息�?
	fmt.Printf("\n�?. 获取实例列表（含详细信息�? %s】\n", testRegion)
	detailedInstances, err := adapter.GetInstancesWithDetails(ctx, testRegion)
	if err != nil {
		fmt.Printf("�?获取详细实例列表失败: %v\n", err)
	} else {
		fmt.Printf("�?获取�?%d 个实例（含详细信息）\n", len(detailedInstances))
		
		// 显示�?个实例的磁盘信息
		for i, inst := range detailedInstances {
			if i >= 3 {
				break
			}
			fmt.Printf("\n  实例 %d: %s (%s)\n", i+1, inst.InstanceID, inst.InstanceName)
			fmt.Printf("    数据盘数�? %d\n", len(inst.DataDisks))
			for j, disk := range inst.DataDisks {
				fmt.Printf("      数据�?%d: %s (%s, %d GB)\n", 
					j+1, disk.DiskID, disk.Category, disk.Size)
			}
		}
	}

	// 5. 获取实例监控数据
	fmt.Println("\n�?. 获取实例监控数据�?)
	endTime := time.Now()
	startTime := endTime.Add(-1 * time.Hour) // 最�?小时
	
	monitorData, err := adapter.GetInstanceMonitorData(
		ctx,
		testRegion,
		testInstanceID,
		startTime.Format("2006-01-02T15:04:05Z"),
		endTime.Format("2006-01-02T15:04:05Z"),
	)
	if err != nil {
		fmt.Printf("�?获取监控数据失败: %v\n", err)
	} else {
		fmt.Printf("�?获取�?%d 个监控数据点\n", len(monitorData.DataPoints))
		
		// 显示最�?个数据点
		if len(monitorData.DataPoints) > 0 {
			fmt.Println("\n  最近的监控数据:")
			count := 3
			if len(monitorData.DataPoints) < count {
				count = len(monitorData.DataPoints)
			}
			
			for i := 0; i < count; i++ {
				dp := monitorData.DataPoints[i]
				fmt.Printf("\n    时间: %s\n", dp.Timestamp)
				fmt.Printf("      CPU使用�?    %.2f%%\n", dp.CPUUtilization)
				fmt.Printf("      内存使用�?   %.2f%%\n", dp.MemoryUtilization)
				fmt.Printf("      公网入流�?   %d KB/s\n", dp.InternetBandwidthIn)
				fmt.Printf("      公网出流�?   %d KB/s\n", dp.InternetBandwidthOut)
				fmt.Printf("      内网入流�?   %d KB/s\n", dp.IntranetBandwidthIn)
				fmt.Printf("      内网出流�?   %d KB/s\n", dp.IntranetBandwidthOut)
			}
		}
	}

	fmt.Println("\n=====================================")
	fmt.Println("🎉 ECS详细信息测试完成�?)
}
