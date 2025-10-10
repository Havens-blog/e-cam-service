package ioc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func InitMongoDB() *mongox.Mongo {
	logger := elog.DefaultLogger
	logger.Info("开始初始化MongoDB连接")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	monitor := &event.CommandMonitor{
		Started: func(ctx context.Context, evt *event.CommandStartedEvent) {
			//fmt.Println(evt.Command)
		},
	}

	type Config struct {
		DSN      string `mapstructure:"dsn"`
		DB       string `mapstructure:"db"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
	}

	var cfg Config
	if err := viper.UnmarshalKey("mongodb", &cfg); err != nil {
		logger.Error("读取MongoDB配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read mongodb config: %w", err))
	}

	// 验证必需的配置项
	if cfg.DSN == "" {
		logger.Error("MongoDB DSN配置缺失")
		panic(fmt.Errorf("mongodb dsn is required but not configured"))
	}
	if cfg.DB == "" {
		logger.Error("MongoDB数据库名配置缺失")
		panic(fmt.Errorf("mongodb database name is required but not configured"))
	}
	if cfg.Username == "" {
		logger.Error("MongoDB用户名配置缺失")
		panic(fmt.Errorf("mongodb username is required but not configured"))
	}
	if cfg.Password == "" {
		logger.Error("MongoDB密码配置缺失")
		panic(fmt.Errorf("mongodb password is required but not configured"))
	}

	// 构建连接URI
	dsn := strings.Split(cfg.DSN, "//")
	if len(dsn) != 2 {
		logger.Error("MongoDB DSN格式无效", elog.String("dsn", cfg.DSN))
		panic(fmt.Errorf("invalid mongodb dsn format: %s", cfg.DSN))
	}

	uri := fmt.Sprintf("%s//%s:%s@%s", dsn[0], cfg.Username, cfg.Password, dsn[1])
	logger.Info("构建MongoDB连接URI", elog.String("db", cfg.DB))

	opts := options.Client().
		ApplyURI(uri).
		SetMonitor(monitor)

	logger.Info("正在连接MongoDB服务器...")
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		logger.Error("连接MongoDB服务器失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to connect to mongodb: %w", err))
	}

	logger.Info("正在测试MongoDB连接...")
	if err = client.Ping(ctx, nil); err != nil {
		logger.Error("MongoDB连接测试失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to ping mongodb server: %w", err))
	}

	logger.Info("MongoDB连接初始化完成", elog.String("database", cfg.DB))
	return mongox.NewMongo(client, cfg.DB)
}
