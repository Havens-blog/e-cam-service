//go:build ignore
// +build ignore

// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/gotomicro/ego/core/elog"
)

func main() {
	logger := elog.DefaultLogger

	fmt.Println("🔌 测试适配器工�?)
	fmt.Println("=====================================")

	// 创建适配器工�?
	factory := adapters.NewAdapterFactory(logger)

	// 方式1: 从云账号配置创建适配�?
	fmt.Println("\n【方�?: 从云账号配置创建�?)
	account := &domain.CloudAccount{
		ID:              1,
		Name:            "测试阿里云账�?,
		Provider:        domain.ProviderAliyun,
		AccessKeyID:     os.Getenv("ALIYUN_ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("ALIYUN_ACCESS_KEY_SECRET"),
		DefaultRegion:   "cn-shenzhen", // 使用深圳作为默认地域
		Enabled:         true,
		Description:     "用于测试的阿里云账号",
	}

	if account.AccessKeyID == "" || account.AccessKeySecret == "" {
		fmt.Println("⚠️  未设置环境变量，使用测试凭证")
		account.AccessKeyID = "test_key"
		account.AccessKeySecret = "test_secret"
	}

	adapter, err := factory.CreateAdapter(account)
	if err != nil {
		fmt.Printf("�?创建适配器失�? %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("�?成功创建适配�? %s\n", adapter.GetProvider())
	fmt.Printf("   账号名称: %s\n", account.Name)
	fmt.Printf("   默认地域: %s\n", account.DefaultRegion)

	// 测试凭证验证
	ctx := context.Background()
	err = adapter.ValidateCredentials(ctx)
	if err != nil {
		fmt.Printf("⚠️  凭证验证失败（预期行为）: %v\n", err)
	} else {
		fmt.Println("�?凭证验证成功")

		// 如果凭证有效，获取地域列�?
		regions, err := adapter.GetRegions(ctx)
		if err != nil {
			fmt.Printf("�?获取地域列表失败: %v\n", err)
		} else {
			fmt.Printf("�?获取�?%d 个地域\n", len(regions))
			if len(regions) > 0 {
				fmt.Println("   �?个地�?")
				for i, region := range regions {
					if i >= 5 {
						break
					}
					fmt.Printf("   - %s (%s)\n", region.ID, region.LocalName)
				}
			}
		}
	}

	// 方式2: 直接通过云厂商类型创建（用于测试�?
	fmt.Println("\n【方�?: 直接通过云厂商类型创建�?)
	adapter2, err := factory.CreateAdapterByProvider(
		domain.ProviderAliyun,
		"test_key",
		"test_secret",
		"cn-beijing", // 使用北京作为默认地域
	)
	if err != nil {
		fmt.Printf("�?创建适配器失�? %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("�?成功创建适配�? %s\n", adapter2.GetProvider())

	// 测试不支持的云厂�?
	fmt.Println("\n【测试不支持的云厂商�?)
	_, err = factory.CreateAdapterByProvider(
		domain.ProviderAWS,
		"test_key",
		"test_secret",
		"us-east-1",
	)
	if err != nil {
		fmt.Printf("�?按预期返回错�? %v\n", err)
	}

	fmt.Println("\n=====================================")
	fmt.Println("🎉 适配器工厂测试完成！")
}
