//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

func main() {
	fmt.Println("🔍 E-CAM Service elog 日志测试")
	fmt.Println("=====================================\n")

	// 使用 ego 框架的默认 logger
	logger := elog.DefaultLogger

	// 基础日志
	logger.Info("这是一条 Info 日志",
		elog.String("key1", "value1"),
		elog.Int("key2", 123))

	logger.Warn("这是一条 Warn 日志",
		elog.String("warning", "something might be wrong"))

	logger.Error("这是一条 Error 日志",
		elog.FieldErr(fmt.Errorf("测试错误")))

	// 测试不同类型的字段
	logger.Info("测试各种字段类型",
		elog.String("string", "字符串"),
		elog.Int("int", 42),
		elog.Int64("int64", 123456789),
		elog.Any("bool", true),
		elog.Any("float", 3.14),
		elog.Any("array", []string{"a", "b", "c"}))

	// 模拟业务日志
	logger.Info("云账号创建成功",
		elog.String("account_id", "acc_123456"),
		elog.String("provider", "aliyun"),
		elog.String("name", "生产环境账号"))

	// 模拟错误日志
	err := fmt.Errorf("数据库连接失败")
	logger.Error("操作失败",
		elog.FieldErr(err),
		elog.String("operation", "create_account"))

	// 模拟耗时操作
	start := time.Now()
	time.Sleep(100 * time.Millisecond)
	elapsed := time.Since(start)

	logger.Info("操作完成",
		elog.String("operation", "sync_assets"),
		elog.Duration("elapsed", elapsed))

	fmt.Println("\n=====================================")
	fmt.Println("✅ elog 日志测试完成！")
	fmt.Println("\n推荐使用方式:")
	fmt.Println("  logger := elog.DefaultLogger")
	fmt.Println("  logger.Info(\"消息\", elog.String(\"key\", \"value\"))")
	fmt.Println("  logger.Error(\"错误\", elog.FieldErr(err))")
}
