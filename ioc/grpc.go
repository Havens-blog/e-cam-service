package ioc

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/grpcx"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

func InitGrpcServer() *grpcx.Server {
	logger := elog.DefaultLogger
	logger.Info("开始初始化gRPC服务器")

	type Config struct {
		Server struct {
			Name    string `mapstructure:"name"`
			Port    int    `mapstructure:"port"`
			EtcdTTL int64  `mapstructure:"etcdTTL"`
			Key     string `mapstructure:"key"`
		} `mapstructure:"server"`
	}

	var cfg Config
	if err := viper.UnmarshalKey("grpc", &cfg); err != nil {
		logger.Error("读取gRPC配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read grpc config: %w", err))
	}

	// 验证必需的配置项
	if cfg.Server.Name == "" {
		logger.Error("gRPC服务器名称配置缺失")
		panic(fmt.Errorf("grpc server name is required but not configured"))
	}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		logger.Error("gRPC服务器端口配置无效", elog.Int("port", cfg.Server.Port))
		panic(fmt.Errorf("invalid grpc server port: %d", cfg.Server.Port))
	}
	if cfg.Server.Key == "" {
		logger.Error("gRPC服务器密钥配置缺失")
		panic(fmt.Errorf("grpc server key is required but not configured"))
	}

	logger.Info("gRPC配置验证通过",
		elog.String("name", cfg.Server.Name),
		elog.Int("port", cfg.Server.Port),
		elog.Int64("etcd_ttl", cfg.Server.EtcdTTL))

	// 初始化etcd客户端
	logger.Info("初始化etcd客户端")
	etcdClient := InitEtcdClient()

	// 创建gRPC服务器
	logger.Info("创建gRPC服务器实例")
	server := grpc.NewServer()

	logger.Info("gRPC服务器初始化完成")
	return &grpcx.Server{
		Server:     server,
		Port:       cfg.Server.Port,
		EtcdTTL:    cfg.Server.EtcdTTL,
		EtcdClient: etcdClient,
		Name:       cfg.Server.Name,
		L:          logger,
	}
}

func InitEtcdClient() *clientv3.Client {
	logger := elog.DefaultLogger
	logger.Info("开始初始化etcd客户端")

	type Config struct {
		Endpoints []string `mapstructure:"endpoints"`
	}

	var cfg Config
	if err := viper.UnmarshalKey("etcd", &cfg); err != nil {
		logger.Error("读取etcd配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read etcd config: %w", err))
	}

	// 验证必需的配置项
	if len(cfg.Endpoints) == 0 {
		logger.Error("etcd端点配置缺失")
		panic(fmt.Errorf("etcd endpoints are required but not configured"))
	}

	logger.Info("etcd配置验证通过", elog.Any("endpoints", cfg.Endpoints))

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		logger.Error("创建etcd客户端失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to create etcd client: %w", err))
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = client.Status(ctx, cfg.Endpoints[0])
	if err != nil {
		logger.Error("etcd连接测试失败", elog.FieldErr(err), elog.String("endpoint", cfg.Endpoints[0]))
		panic(fmt.Errorf("failed to connect to etcd: %w", err))
	}
	logger.Info("etcd连接测试成功")

	logger.Info("etcd客户端初始化完成")
	return client
}
