//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/gotomicro/ego/core/elog"
)

func main() {
	// 初始化日志
	logger := elog.DefaultLogger

	// 从环境变量获取阿里云凭证
	accessKeyID := os.Getenv("ALIYUN_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIYUN_ACCESS_KEY_SECRET")

	if accessKeyID == "" || accessKeySecret == "" {
		fmt.Println("❌ 请设置环境变量 ALIYUN_ACCESS_KEY_ID 和 ALIYUN_ACCESS_KEY_SECRET")
		os.Exit(1)
	}

	fmt.Println("🔌 测试阿里云适配器")
	fmt.Println("=====================================")

	// 创建适配器
	config := adapters.AliyunConfig{
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
	}
	adapter := adapters.NewAliyunAdapter(config, logger)

	ctx := context.Background()

	// 1. 验证凭证
	fmt.Println("\n【1. 验证凭证】")
	if err := adapter.ValidateCredentials(ctx); err != nil {
		fmt.Printf("❌ 凭证验证失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 凭证验证成功")

	// 2. 获取地域列表
	fmt.Println("\n【2. 获取地域列表】")
	regions, err := adapter.GetRegions(ctx)
	if err != nil {
		fmt.Printf("❌ 获取地域列表失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 获取到 %d 个地域:\n", len(regions))
	for i, region := range regions {
		if i < 5 { // 只显示前5个
			fmt.Printf("  - %s (%s)\n", region.ID, region.LocalName)
		}
	}
	if len(regions) > 5 {
		fmt.Printf("  ... 还有 %d 个地域\n", len(regions)-5)
	}

	// 3. 获取ECS实例（测试一个地域）
	testRegion := "cn-hangzhou"
	fmt.Printf("\n【3. 获取ECS实例 - %s】\n", testRegion)
	instances, err := adapter.GetECSInstances(ctx, testRegion)
	if err != nil {
		fmt.Printf("❌ 获取ECS实例失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 获取到 %d 个ECS实例\n", len(instances))

	if len(instances) > 0 {
		fmt.Println("\n前3个实例详情:")
		for i, inst := range instances {
			if i >= 3 {
				break
			}
			fmt.Printf("\n实例 %d:\n", i+1)
			fmt.Printf("  ID:              %s\n", inst.InstanceID)
			fmt.Printf("  名称:            %s\n", inst.InstanceName)
			fmt.Printf("  状态:            %s\n", inst.Status)
			fmt.Printf("  地域:            %s\n", inst.Region)
			fmt.Printf("  可用区:          %s\n", inst.Zone)
			fmt.Printf("  实例规格:        %s\n", inst.InstanceType)
			fmt.Printf("  规格族:          %s\n", inst.InstanceTypeFamily)
			fmt.Printf("  CPU:             %d 核\n", inst.CPU)
			fmt.Printf("  内存:            %d MB\n", inst.Memory)
			fmt.Printf("  操作系统:        %s (%s)\n", inst.OSName, inst.OSType)
			fmt.Printf("  镜像ID:          %s\n", inst.ImageID)
			fmt.Printf("  公网IP:          %s\n", inst.PublicIP)
			fmt.Printf("  私网IP:          %s\n", inst.PrivateIP)
			fmt.Printf("  VPC ID:          %s\n", inst.VPCID)
			fmt.Printf("  交换机ID:        %s\n", inst.VSwitchID)
			fmt.Printf("  安全组:          %v\n", inst.SecurityGroups)
			fmt.Printf("  入网带宽:        %d Mbps\n", inst.InternetMaxBandwidthIn)
			fmt.Printf("  出网带宽:        %d Mbps\n", inst.InternetMaxBandwidthOut)
			fmt.Printf("  系统盘类型:      %s\n", inst.SystemDiskCategory)
			fmt.Printf("  系统盘大小:      %d GB\n", inst.SystemDiskSize)
			if len(inst.DataDisks) > 0 {
				fmt.Printf("  数据盘数量:      %d\n", len(inst.DataDisks))
			}
			fmt.Printf("  计费方式:        %s\n", inst.ChargeType)
			fmt.Printf("  创建时间:        %s\n", inst.CreationTime)
			fmt.Printf("  网络类型:        %s\n", inst.NetworkType)
			fmt.Printf("  IO优化:          %s\n", inst.IoOptimized)
			fmt.Printf("  主机名:          %s\n", inst.HostName)
			if inst.KeyPairName != "" {
				fmt.Printf("  密钥对:          %s\n", inst.KeyPairName)
			}
			if len(inst.Tags) > 0 {
				fmt.Printf("  标签:            %v\n", inst.Tags)
			}
		}

		// 测试获取单个实例详情
		if len(instances) > 0 {
			fmt.Printf("\n【4. 获取单个实例详情】\n")
			firstInstanceID := instances[0].InstanceID
			detail, err := adapter.GetECSInstanceDetail(ctx, testRegion, firstInstanceID)
			if err != nil {
				fmt.Printf("❌ 获取实例详情失败: %v\n", err)
			} else {
				fmt.Printf("✅ 成功获取实例 %s 的详细信息\n", detail.InstanceID)
			}
		}
	}

	fmt.Println("\n=====================================")
	fmt.Println("🎉 阿里云适配器测试完成！")
}
