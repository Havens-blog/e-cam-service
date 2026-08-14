// Package mcp 提供基于 Model Context Protocol 的多云资产管理 MCP Server
package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camdao "github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Dependencies MCP Server 所需的全部依赖
type Dependencies struct {
	AccountSvc  accountservice.CloudAccountService
	InstanceSvc camservice.InstanceService
	Factory     *cloudx.AdapterFactory
	Logger      *elog.Component
	mongoDB     *mongox.Mongo
}

// Close 关闭依赖资源
func (d *Dependencies) Close() {
	if d.mongoDB != nil && d.mongoDB.DBClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.mongoDB.DBClient.Disconnect(ctx)
	}
}

// InitDependencies 初始化 MCP Server 所需的全部依赖
func InitDependencies() (*Dependencies, error) {
	logger := elog.DefaultLogger
	if logger == nil {
		logger = elog.EgoLogger
	}

	// 初始化 MongoDB
	mongoDB, err := initMongoDB()
	if err != nil {
		return nil, fmt.Errorf("初始化 MongoDB 失败: %w", err)
	}

	// 初始化 account 层
	accountDAO := dao.NewCloudAccountDAO(mongoDB)
	accountRepo := repository.NewCloudAccountRepository(accountDAO)
	accountSvc := accountservice.NewCloudAccountService(accountRepo, nil, logger)

	// 初始化 instance 层
	instanceDAO := camdao.NewInstanceDAO(mongoDB)
	instanceRepo := camrepository.NewInstanceRepository(instanceDAO)
	instanceSvc := camservice.NewInstanceService(instanceRepo)

	// 初始化 cloudx 工厂
	factory := cloudx.NewAdapterFactory(logger)

	return &Dependencies{
		AccountSvc:  accountSvc,
		InstanceSvc: instanceSvc,
		Factory:     factory,
		Logger:      logger,
		mongoDB:     mongoDB,
	}, nil
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

// listInstances 通用的资产列表查询辅助方法
func (d *Dependencies) listInstances(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error) {
	return d.InstanceSvc.List(ctx, filter)
}

// searchInstances 通用的资产搜索辅助方法
func (d *Dependencies) searchInstances(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error) {
	return d.InstanceSvc.Search(ctx, filter)
}
