package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/domain"
)

func main() {
	// 创建阿里云验证器
	factory := cloudx.NewCloudValidatorFactory()
	validator, err := factory.CreateValidator(domain.CloudProviderAliyun)
	if err != nil {
		log.Fatalf("创建验证器失败: %v", err)
	}

	// 示例账号信息（请替换为真实的凭证进行测试）
	account := &domain.CloudAccount{
		Provider:        domain.CloudProviderAliyun,
		AccessKeyID:     "LTAI5tFakeKeyForTesting1",       // 请替换为真实的 AccessKeyId
		AccessKeySecret: "FakeSecretKeyForTestingPurpose", // 请替换为真实的 AccessKeySecret
		Region:          "cn-hangzhou",
	}

	// 创建带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("开始验证阿里云凭证...")

	// 1. 验证凭证
	result, err := validator.ValidateCredentials(ctx, account)
	if err != nil {
		log.Fatalf("验证过程出错: %v", err)
	}

	// 输出验证结果
	fmt.Printf("验证结果:\n")
	fmt.Printf("  有效性: %t\n", result.Valid)
	fmt.Printf("  消息: %s\n", result.Message)
	fmt.Printf("  响应时间: %d ms\n", result.ResponseTime)
	fmt.Printf("  账号信息: %s\n", result.AccountInfo)

	if result.Valid {
		fmt.Printf("  支持的地域数量: %d\n", len(result.Regions))
		fmt.Printf("  前5个地域: %v\n", result.Regions[:min(5, len(result.Regions))])
		fmt.Printf("  检测到的权限: %v\n", result.Permissions)

		// 2. 获取完整的地域列表
		fmt.Println("\n获取支持的地域列表...")
		regions, err := validator.GetSupportedRegions(ctx, account)
		if err != nil {
			fmt.Printf("获取地域列表失败: %v\n", err)
		} else {
			fmt.Printf("支持的地域总数: %d\n", len(regions))
			for i, region := range regions {
				if i < 10 { // 只显示前10个
					fmt.Printf("  - %s\n", region)
				}
			}
			if len(regions) > 10 {
				fmt.Printf("  ... 还有 %d 个地域\n", len(regions)-10)
			}
		}

		// 3. 测试连接
		fmt.Println("\n测试连接...")
		err = validator.TestConnection(ctx, account)
		if err != nil {
			fmt.Printf("连接测试失败: %v\n", err)
		} else {
			fmt.Println("连接测试成功!")
		}
	} else {
		fmt.Println("凭证验证失败，请检查 AccessKeyId 和 AccessKeySecret")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
