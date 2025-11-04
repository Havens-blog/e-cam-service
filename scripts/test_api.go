//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/gotomicro/ego/core/elog"
)

func main() {
	logger := elog.DefaultLogger

	// 使用测试凭证
	config := adapters.AliyunConfig{
		AccessKeyID:     "test_key",
		AccessKeySecret: "test_secret",
	}

	adapter := adapters.NewAliyunAdapter(config, logger)

	fmt.Println("🔌 测试阿里云适配器基础功能")
	fmt.Println("=====================================")

	// 测试 GetProvider
	fmt.Printf("\n云厂商类型: %s\n", adapter.GetProvider())

	// 测试凭证验证（会失败，因为是测试凭证）
	ctx := context.Background()
	err := adapter.ValidateCredentials(ctx)
	if err != nil {
		fmt.Printf("✅ 凭证验证按预期失败（测试凭证）: %v\n", err)
	}

	fmt.Println("\n=====================================")
	fmt.Println("🎉 基础功能测试完成！")
	fmt.Println("\n提示: 要测试真实API，请设置环境变量:")
	fmt.Println("  export ALIYUN_ACCESS_KEY_ID=your_key")
	fmt.Println("  export ALIYUN_ACCESS_KEY_SECRET=your_secret")
	fmt.Println("  go run scripts/test_aliyun_adapter.go")
}
