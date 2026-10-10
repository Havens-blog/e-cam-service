package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	accountdao "github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camdao "github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/mcp"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// buildDependencies 为 MCP Server 装配全部依赖（命令层组合根）：自建独立
// MongoDB 连接，按 dao → repository → service 装配 account/cam 领域，并构造
// cloudx 工厂，最终注入 mcp.NewDependencies。仓储/DAO 的 import 止于本命令层，
// internal/mcp 领域包仅消费服务接口（depcheck R1）。
func buildDependencies() (*mcp.Dependencies, error) {
	logger := elog.DefaultLogger
	if logger == nil {
		logger = elog.EgoLogger
	}

	mongoDB, err := initMongoDB()
	if err != nil {
		return nil, fmt.Errorf("初始化 MongoDB 失败: %w", err)
	}

	// account 层：dao → repository → service
	accountSvc := accountservice.NewCloudAccountService(
		accountrepo.NewCloudAccountRepository(accountdao.NewCloudAccountDAO(mongoDB)),
		nil,
		logger,
	)

	// instance 层：dao → repository → service（cam 域）
	instanceSvc := camservice.NewInstanceService(
		camrepo.NewInstanceRepository(camdao.NewInstanceDAO(mongoDB)),
	)

	factory := cloudx.NewAdapterFactory(logger)

	return mcp.NewDependencies(accountSvc, instanceSvc, factory, logger, mongoDB), nil
}

// initMongoDB 初始化 MongoDB 连接（独立于主服务的 ioc）
func initMongoDB() (*mongox.Mongo, error) {
	type Config struct {
		DSN      string `mapstructure:"dsn"`
		DB       string `mapstructure:"db"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
	}

	var cfg Config
	if err := viper.UnmarshalKey("mongodb", &cfg); err != nil {
		return nil, fmt.Errorf("读取 MongoDB 配置失败: %w", err)
	}

	if cfg.DSN == "" || cfg.DB == "" {
		return nil, fmt.Errorf("MongoDB DSN 或数据库名未配置")
	}

	dsn := strings.Split(cfg.DSN, "//")
	if len(dsn) != 2 {
		return nil, fmt.Errorf("MongoDB DSN 格式无效: %s", cfg.DSN)
	}

	uri := fmt.Sprintf("%s//%s:%s@%s", dsn[0], cfg.Username, cfg.Password, dsn[1])

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("连接 MongoDB 失败: %w", err)
	}

	if err = client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("MongoDB Ping 失败: %w", err)
	}

	return mongox.NewMongo(client, cfg.DB), nil
}
