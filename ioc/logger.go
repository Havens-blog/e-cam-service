package ioc

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LoggerConfig 日志配置
type LoggerConfig struct {
	Level            string        `mapstructure:"level"`
	Format           string        `mapstructure:"format"`
	EnableCaller     bool          `mapstructure:"enableCaller"`
	CallerSkip       int           `mapstructure:"callerSkip"`
	EnableStacktrace bool          `mapstructure:"enableStacktrace"`
	TimeFormat       string        `mapstructure:"timeFormat"`
	OutputPaths      []string      `mapstructure:"outputPaths"`
	ErrorOutputPaths []string      `mapstructure:"errorOutputPaths"`
	EncoderConfig    EncoderConfig `mapstructure:"encoderConfig"`
}

// EncoderConfig 编码器配置
type EncoderConfig struct {
	TimeKey         string `mapstructure:"timeKey"`
	LevelKey        string `mapstructure:"levelKey"`
	NameKey         string `mapstructure:"nameKey"`
	CallerKey       string `mapstructure:"callerKey"`
	MessageKey      string `mapstructure:"messageKey"`
	StacktraceKey   string `mapstructure:"stacktraceKey"`
	LevelEncoder    string `mapstructure:"levelEncoder"`
	TimeEncoder     string `mapstructure:"timeEncoder"`
	DurationEncoder string `mapstructure:"durationEncoder"`
	CallerEncoder   string `mapstructure:"callerEncoder"`
}

// InitLogger 初始化日志系统
func InitLogger() *zap.Logger {
	// 读取日志配置
	var cfg LoggerConfig
	if err := viper.UnmarshalKey("logger.default", &cfg); err != nil {
		// 如果配置读取失败，使用默认配置
		fmt.Printf("⚠️  读取日志配置失败，使用默认配置: %v\n", err)
		return initDefaultLogger()
	}

	// 创建日志目录
	for _, path := range cfg.OutputPaths {
		if path != "stdout" && path != "stderr" {
			dir := path[:len(path)-len(path[len(path)-1:])]
			if dir != "" {
				os.MkdirAll("logs", 0755)
			}
		}
	}

	// 构建 zap 配置
	zapConfig := buildZapConfig(cfg)

	// 创建 logger
	logger, err := zapConfig.Build(
		zap.AddCaller(),
		zap.AddCallerSkip(cfg.CallerSkip),
	)
	if err != nil {
		fmt.Printf("❌ 创建日志器失败: %v\n", err)
		return initDefaultLogger()
	}

	logger.Info("日志系统初始化完成",
		zap.String("level", cfg.Level),
		zap.String("format", cfg.Format),
		zap.Bool("enableCaller", cfg.EnableCaller))

	return logger
}

// buildZapConfig 构建 zap 配置
func buildZapConfig(cfg LoggerConfig) zap.Config {
	// 基础配置
	zapConfig := zap.NewProductionConfig()

	// 设置日志级别
	level := parseLogLevel(cfg.Level)
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// 设置编码格式
	zapConfig.Encoding = cfg.Format
	if cfg.Format == "" {
		zapConfig.Encoding = "console"
	}

	// 设置输出路径
	if len(cfg.OutputPaths) > 0 {
		zapConfig.OutputPaths = cfg.OutputPaths
	}
	if len(cfg.ErrorOutputPaths) > 0 {
		zapConfig.ErrorOutputPaths = cfg.ErrorOutputPaths
	}

	// 配置编码器
	zapConfig.EncoderConfig = buildEncoderConfig(cfg.EncoderConfig, cfg.TimeFormat)

	// 禁用采样（确保所有日志都被记录）
	zapConfig.Sampling = nil

	return zapConfig
}

// buildEncoderConfig 构建编码器配置
func buildEncoderConfig(cfg EncoderConfig, timeFormat string) zapcore.EncoderConfig {
	encoderConfig := zap.NewProductionEncoderConfig()

	// 设置键名
	if cfg.TimeKey != "" {
		encoderConfig.TimeKey = cfg.TimeKey
	}
	if cfg.LevelKey != "" {
		encoderConfig.LevelKey = cfg.LevelKey
	}
	if cfg.NameKey != "" {
		encoderConfig.NameKey = cfg.NameKey
	}
	if cfg.CallerKey != "" {
		encoderConfig.CallerKey = cfg.CallerKey
	}
	if cfg.MessageKey != "" {
		encoderConfig.MessageKey = cfg.MessageKey
	}
	if cfg.StacktraceKey != "" {
		encoderConfig.StacktraceKey = cfg.StacktraceKey
	}

	// 设置编码器
	// 时间编码器
	if timeFormat != "" {
		encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(timeFormat)
	} else {
		encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	}

	// 级别编码器
	switch cfg.LevelEncoder {
	case "capital":
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	case "capitalColor":
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	case "color":
		encoderConfig.EncodeLevel = zapcore.LowercaseColorLevelEncoder
	default:
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	}

	// 调用者编码器
	switch cfg.CallerEncoder {
	case "short":
		encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	case "full":
		encoderConfig.EncodeCaller = zapcore.FullCallerEncoder
	default:
		encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	}

	// 持续时间编码器
	switch cfg.DurationEncoder {
	case "string":
		encoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	case "nanos":
		encoderConfig.EncodeDuration = zapcore.NanosDurationEncoder
	case "ms":
		encoderConfig.EncodeDuration = zapcore.MillisDurationEncoder
	default:
		encoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	}

	return encoderConfig
}

// parseLogLevel 解析日志级别
func parseLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// initDefaultLogger 初始化默认日志器
func initDefaultLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.Encoding = "console"
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build(zap.AddCaller(), zap.AddCallerSkip(2))
	if err != nil {
		panic(fmt.Sprintf("无法创建默认日志器: %v", err))
	}

	return logger
}
