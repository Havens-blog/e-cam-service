package ioc

import (
	"fmt"
	"os"

	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// InitCustomLogger 初始化自定义日志组件
// 使用 ego 框架的 elog，配置可读时间格式和调用者信息
func InitCustomLogger() {
	// 确保日志目录存在
	os.MkdirAll("logs", 0755)

	// 从配置读取日志级别
	levelStr := "info"
	if viper.IsSet("logger.default.level") {
		levelStr = viper.GetString("logger.default.level")
	}

	// 创建自定义的 encoder 配置 - 可读时间格式
	encoderConfig := &zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,                        // INFO, WARN, ERROR
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05"), // 可读时间格式
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder, // 短路径 file.go:123
	}

	// 使用 ego 的 elog 创建 logger
	elog.DefaultLogger = elog.DefaultContainer().Build(
		elog.WithEncoderConfig(encoderConfig),
		elog.WithLevel(levelStr),
		elog.WithEnableAddCaller(true),
		elog.WithFileName("logs/default.log"),
	)

	fmt.Println("✅ 日志系统初始化完成 (ego elog)")
}

// InitLogger 初始化日志系统 (保留以兼容 wire)
func InitLogger() *zap.Logger {
	// 调用 InitCustomLogger 初始化 ego logger
	InitCustomLogger()
	// 返回 ego logger 的底层 zap logger
	return elog.DefaultLogger.ZapLogger()
}
