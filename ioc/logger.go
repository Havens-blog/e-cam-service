package ioc

import (
	"fmt"
	"os"

	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// InitCustomLogger 初始化日志组件
// 使用 ego elog 作为日志门面，lumberjack 做日志切割
func InitCustomLogger() {
	os.MkdirAll("logs", 0755)

	levelStr := viper.GetString("logger.default.level")
	if levelStr == "" {
		levelStr = "info"
	}

	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(levelStr)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	// encoder 配置：可读时间 + 大写级别 + 短路径调用者
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05"),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	encoder := zapcore.NewConsoleEncoder(encoderConfig)

	// 日志切割：按大小自动轮转
	fileWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   "logs/app.log",
		MaxSize:    200, // 单文件最大 200MB
		MaxAge:     30,  // 保留 30 天
		MaxBackups: 10,  // 最多 10 个备份
		LocalTime:  true,
		Compress:   true,
	})

	// 只输出到文件
	core := zapcore.NewCore(encoder, fileWriter, zapLevel)

	// 注入到 ego elog，保持全项目统一使用 elog.DefaultLogger
	elog.DefaultLogger = elog.DefaultContainer().Build(
		elog.WithZapCore(core),
		elog.WithEnableAddCaller(true),
	)

	fmt.Printf("✅ 日志初始化完成 (level=%s, file=logs/app.log, 切割=200MB, 保留30天)\n", levelStr)
}

// InitLogger 兼容 wire 依赖注入
func InitLogger() *zap.Logger {
	InitCustomLogger()
	return elog.DefaultLogger.ZapLogger()
}
