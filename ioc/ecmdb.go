package ioc

import (
	"fmt"

	endpointv1 "github.com/Havens-blog/e-cam-service/api/proto/gen/ecmdb/endpoint/v1"
	policyv1 "github.com/Havens-blog/e-cam-service/api/proto/gen/ecmdb/policy/v1"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/grpcx/interceptors/jwt"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InitEcmdbPolicyClient 初始化 ecmdb 的 Policy gRPC 客户端
func InitEcmdbPolicyClient(etcdClient *clientv3.Client) policyv1.PolicyServiceClient {
	logger := elog.DefaultLogger
	logger.Info("开始初始化 ecmdb Policy gRPC 客户端")

	type Config struct {
		Target string `mapstructure:"target"`
		Secure bool   `mapstructure:"secure"`
		Key    string `mapstructure:"key"`
	}
	var cfg Config
	err := viper.UnmarshalKey("grpc.client.ecmdb", &cfg)
	if err != nil {
		logger.Error("读取 ecmdb gRPC 客户端配置失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to read ecmdb grpc client config: %w", err))
	}

	if cfg.Target == "" {
		panic("ecmdb grpc client target is required")
	}
	if cfg.Key == "" {
		panic("ecmdb grpc client key is required")
	}

	rs, err := resolver.NewBuilder(etcdClient)
	if err != nil {
		logger.Error("创建 etcd resolver 失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to create etcd resolver: %w", err))
	}

	// 创建 JWT 客户端拦截器，与 ecmdb 的 gRPC 服务端共享密钥
	jwtInterceptor := jwt.NewClientInterceptorBuilder(cfg.Key)
	opts := []grpc.DialOption{
		grpc.WithResolvers(rs),
		grpc.WithUnaryInterceptor(jwtInterceptor.UnaryClientInterceptor()),
	}

	if !cfg.Secure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	cc, err := grpc.NewClient(cfg.Target, opts...)
	if err != nil {
		logger.Error("创建 ecmdb gRPC 连接失败", elog.FieldErr(err))
		panic(fmt.Errorf("failed to create ecmdb grpc client: %w", err))
	}

	logger.Info("ecmdb Policy gRPC 客户端初始化完成", elog.String("target", cfg.Target))
	return policyv1.NewPolicyServiceClient(cc)
}

// InitEcmdbEndpointClient 初始化 ecmdb 的 Endpoint gRPC 客户端
func InitEcmdbEndpointClient(etcdClient *clientv3.Client) endpointv1.EndpointServiceClient {
	logger := elog.DefaultLogger
	logger.Info("开始初始化 ecmdb Endpoint gRPC 客户端")

	type Config struct {
		Target string `mapstructure:"target"`
		Secure bool   `mapstructure:"secure"`
		Key    string `mapstructure:"key"`
	}
	var cfg Config
	err := viper.UnmarshalKey("grpc.client.ecmdb", &cfg)
	if err != nil {
		panic(fmt.Errorf("failed to read ecmdb grpc client config: %w", err))
	}

	rs, err := resolver.NewBuilder(etcdClient)
	if err != nil {
		panic(fmt.Errorf("failed to create etcd resolver: %w", err))
	}

	jwtInterceptor := jwt.NewClientInterceptorBuilder(cfg.Key)
	opts := []grpc.DialOption{
		grpc.WithResolvers(rs),
		grpc.WithUnaryInterceptor(jwtInterceptor.UnaryClientInterceptor()),
	}

	if !cfg.Secure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	cc, err := grpc.NewClient(cfg.Target, opts...)
	if err != nil {
		panic(fmt.Errorf("failed to create ecmdb grpc client: %w", err))
	}

	logger.Info("ecmdb Endpoint gRPC 客户端初始化完成")
	return endpointv1.NewEndpointServiceClient(cc)
}

// InitCheckPolicyMiddleware 初始化策略检查中间件
func InitCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient) *middleware.CheckPolicyMiddleware {
	var cfg middleware.PolicyConfig
	_ = viper.UnmarshalKey("policy", &cfg)
	return middleware.NewCheckPolicyMiddleware(policyClient, cfg, elog.DefaultLogger)
}
