//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	fmt.Println("🔍 E-CAM Service 日志配置测试")
	fmt.Println("=====================================\n")

	// 创建日志目录
	os.MkdirAll("logs", 0755)

	// 测试 Console 格式（开发环境推荐）
	fmt.Println("📝 测试 Console 格式（开发环境）")
	fmt.Println("-------------------------------------")
	testConsoleLogger()

	fmt.Println("\n📝 测试 JSON 格式（生产环境）")
	fmt.Println("-------------------------------------")
	testJSONLogger()

	fmt.Println("\n=====================================")
	fmt.Println("✅ 日志测试完成！")
	fmt.Println("\n请检查文件:")
	fmt.Println("  - logs/test_console.log  (Console 格式)")
	fmt.Println("  - logs/test_json.log     (JSON 格式)")
	fmt.Println("  - logs/test_error.log    (错误日志)")
	fmt.Println("\n📚 查看完整文档: docs/logger-configuration.md")
}

// testConsoleLogger 测试 Console 格式日志
func testConsoleLogger() {
	logger := createConsoleLogger()
	defer logger.Sync()

	// 模拟服务启动日志
	logger.Info("服务启动成功",
		zap.String("service", "e-cam-service"),
		zap.String("version", "1.0.0"),
		zap.Int("port", 8001))

	// 模拟数据库连接日志
	logger.Info("开始初始化MongoDB连接",
		zap.String("host", "localhost:27017"),
		zap.String("database", "e_cam_service"))

	logger.Info("MongoDB连接初始化完成",
		zap.String("database", "e_cam_service"),
		zap.Duration("elapsed", 150*time.Millisecond))

	// 模拟业务操作日志
	testBusinessOperations(logger)

	// 模拟警告日志
	logger.Warn("云账号连接测试失败",
		zap.String("account_id", "acc_123456"),
		zap.String("provider", "aliyun"),
		zap.String("reason", "timeout"))

	// 模拟错误日志
	logger.Error("创建资产失败",
		zap.String("asset_id", "asset_789"),
		zap.String("asset_name", "web-server-01"),
		zap.Error(fmt.Errorf("invalid input: missing required field 'provider'")))

	// 测试调用者信息
	testCallerInfo(logger)
}

// testJSONLogger 测试 JSON 格式日志
func testJSONLogger() {
	logger := createJSONLogger()
	defer logger.Sync()

	logger.Info("服务启动",
		zap.String("service", "e-cam-service"),
		zap.String("environment", "production"))

	logger.Info("处理请求",
		zap.String("request_id", "req_abc123"),
		zap.String("method", "POST"),
		zap.String("path", "/api/v1/cam/assets"),
		zap.String("user_id", "user_456"))

	logger.Error("请求处理失败",
		zap.String("request_id", "req_abc123"),
		zap.Int("status_code", 500),
		zap.Error(fmt.Errorf("database connection lost")))
}

// testBusinessOperations 测试业务操作日志
func testBusinessOperations(logger *zap.Logger) {
	ctx := context.Background()

	// 模拟资产创建
	logger.Info("开始创建资产",
		zap.String("asset_name", "web-server-01"),
		zap.String("provider", "aliyun"),
		zap.String("region", "cn-hangzhou"))

	// 模拟耗时操作
	start := time.Now()
	time.Sleep(100 * time.Millisecond)
	elapsed := time.Since(start)

	logger.Info("资产创建完成",
		zap.String("asset_id", "asset_123456"),
		zap.String("asset_name", "web-server-01"),
		zap.Duration("elapsed", elapsed))

	// 模拟资产同步
	logger.Info("开始同步云账号资产",
		zap.String("account_id", "acc_123456"),
		zap.String("provider", "aliyun"),
		zap.Strings("resource_types", []string{"ecs", "rds", "oss"}))

	// 模拟异步任务
	testAsyncTask(ctx, logger)
}

// testAsyncTask 测试异步任务日志
func testAsyncTask(ctx context.Context, logger *zap.Logger) {
	taskID := "task_abc123"

	logger.Info("提交异步任务",
		zap.String("task_id", taskID),
		zap.String("task_type", "sync_assets"),
		zap.String("account_id", "acc_123456"))

	logger.Info("任务开始执行",
		zap.String("task_id", taskID),
		zap.String("status", "running"))

	// 模拟任务进度
	for i := 1; i <= 3; i++ {
		logger.Info("任务执行中",
			zap.String("task_id", taskID),
			zap.Int("progress", i*33),
			zap.String("current_step", fmt.Sprintf("处理资源类型 %d/3", i)))
		time.Sleep(50 * time.Millisecond)
	}

	logger.Info("任务执行完成",
		zap.String("task_id", taskID),
		zap.String("status", "completed"),
		zap.Int("total_resources", 156),
		zap.Duration("elapsed", 300*time.Millisecond))
}

// testCallerInfo 测试调用者信息
func testCallerInfo(logger *zap.Logger) {
	logger.Info("测试调用者信息显示",
		zap.String("function", "testCallerInfo"),
		zap.String("file", "test_simple_logger.go"))
}

// createConsoleLogger 创建 Console 格式的 logger
func createConsoleLogger() *zap.Logger {
	config := zap.NewProductionConfig()

	// Console 格式配置
	config.Encoding = "console"
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	// 编码器配置
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.StacktraceKey = "stacktrace"

	// 格式化配置
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	config.EncoderConfig.EncodeDuration = zapcore.StringDurationEncoder

	// 输出路径
	config.OutputPaths = []string{"stdout", "logs/test_console.log"}
	config.ErrorOutputPaths = []string{"stderr", "logs/test_error.log"}

	logger, err := config.Build(zap.AddCaller(), zap.AddCallerSkip(0))
	if err != nil {
		panic(fmt.Sprintf("创建 logger 失败: %v", err))
	}

	return logger
}

// createJSONLogger 创建 JSON 格式的 logger
func createJSONLogger() *zap.Logger {
	config := zap.NewProductionConfig()

	// JSON 格式配置
	config.Encoding = "json"
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	// 编码器配置
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.MessageKey = "msg"

	// 格式化配置
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// 输出路径
	config.OutputPaths = []string{"stdout", "logs/test_json.log"}
	config.ErrorOutputPaths = []string{"stderr", "logs/test_error.log"}

	logger, err := config.Build(zap.AddCaller(), zap.AddCallerSkip(0))
	if err != nil {
		panic(fmt.Sprintf("创建 logger 失败: %v", err))
	}

	return logger
}
