//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"time"

	"github.com/gotomicro/ego"
	"github.com/gotomicro/ego/core/elog"
)

func main() {
	// 初始化 ego 应用
	app := ego.New(
		ego.WithConfigPath("config/prod.yaml"),
	)

	// 获取日志实例
	logger := elog.DefaultLogger

	fmt.Println("🔍 测试日志配置")
	fmt.Println("=====================================")

	// 测试不同级别的日志
	logger.Debug("这是一条 DEBUG 日志")
	logger.Info("这是一条 INFO 日志")
	logger.Warn("这是一条 WARN 日志")
	logger.Error("这是一条 ERROR 日志")

	// 测试带字段的日志
	logger.Info("测试带字段的日志",
		elog.String("user", "admin"),
		elog.Int("age", 30),
		elog.String("action", "login"))

	// 测试错误日志
	err := fmt.Errorf("这是一个测试错误")
	logger.Error("发生错误", elog.FieldErr(err))

	// 测试调用者信息（文件名和行号）
	testFunction()

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n=====================================")
	fmt.Println("✅ 日志测试完成！")
	fmt.Println("请检查以下文件:")
	fmt.Println("  - logs/default.log")
	fmt.Println("  - logs/error.log")
	fmt.Println("\n日志格式应该包含:")
	fmt.Println("  1. 时间: 2025-10-30 16:07:34 格式")
	fmt.Println("  2. 级别: INFO, WARN, ERROR 等")
	fmt.Println("  3. 调用者: 文件名:行号")
	fmt.Println("  4. 消息内容")

	// 优雅关闭
	app.Stop()
}

func testFunction() {
	logger := elog.DefaultLogger
	logger.Info("这条日志应该显示 testFunction 的文件名和行号")

	// 嵌套调用
	nestedFunction()
}

func nestedFunction() {
	logger := elog.DefaultLogger
	logger.Warn("这条日志应该显示 nestedFunction 的文件名和行号")
}
