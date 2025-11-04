//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"

	"github.com/Havens-blog/e-cam-service/ioc"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func main() {
	fmt.Println("🔍 测试日志系统集成")
	fmt.Println("=====================================\n")

	// 初始化配置
	viper.SetConfigFile("config/prod.yaml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("❌ 读取配置文件失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ 配置文件加载成功")

	// 初始化日志系统
	logger := ioc.InitLogger()
	fmt.Println("✅ 日志系统初始化成功\n")

	// 测试不同级别的日志
	fmt.Println("📝 测试日志输出:")
	fmt.Println("-------------------------------------")

	logger.Info("这是一条 INFO 日志",
		zap.String("module", "test"),
		zap.String("action", "logger_integration"))

	logger.Info("模拟服务启动",
		zap.String("service", "e-cam-service"),
		zap.Int("port", 8001))

	logger.Warn("这是一条 WARN 日志",
		zap.String("warning_type", "test_warning"))

	logger.Error("这是一条 ERROR 日志",
		zap.String("error_type", "test_error"),
		zap.Error(fmt.Errorf("模拟错误")))

	// 测试结构化日志
	logger.Info("测试结构化日志",
		zap.String("user_id", "12345"),
		zap.String("action", "create_asset"),
		zap.String("asset_name", "web-server-01"),
		zap.String("provider", "aliyun"),
		zap.Int("count", 100))

	fmt.Println("\n=====================================")
	fmt.Println("✅ 日志集成测试完成！")
	fmt.Println("\n请检查日志文件:")
	fmt.Println("  - logs/app.log")
	fmt.Println("  - logs/error.log")
	fmt.Println("\n日志格式示例:")
	fmt.Println("  2025-11-04 15:30:45  INFO  caller=scripts/test_logger_integration.go:35  这是一条 INFO 日志  module=test action=logger_integration")
}
