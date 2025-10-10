package ioc

import (
	"fmt"

	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

func InitRedis() redis.Cmdable {
	logger := elog.DefaultLogger
	logger.Info("开始初始化Redis客户端")

	type Config struct {
		Addr     string `mapstructure:"addr"`
		Password string `mapstructure:"password"`
		DB       int    `mapstructure:"db"`
	}
	var cfg Config

	err := viper.UnmarshalKey("redis", &cfg)
	if err != nil {
		logger.Error("读取Redis配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read redis config: %w", err))
	}

	// 验证必需的配置项
	if cfg.Addr == "" {
		logger.Error("Redis地址配置缺失")
		panic(fmt.Errorf("redis address is required but not configured"))
	}

	options := &redis.Options{
		Addr: cfg.Addr,
		DB:   cfg.DB,
	}

	// Only set password if it's not empty
	if cfg.Password != "" {
		options.Password = cfg.Password
		logger.Info("Redis配置了密码认证")
	} else {
		logger.Info("Redis未配置密码认证")
	}

	logger.Info("创建Redis客户端", elog.String("addr", cfg.Addr), elog.Int("db", cfg.DB))
	client := redis.NewClient(options)

	logger.Info("Redis客户端初始化完成")
	return client
}
