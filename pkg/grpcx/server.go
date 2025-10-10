package grpcx

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/netx"
	"github.com/gotomicro/ego/core/elog"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/endpoints"
	"google.golang.org/grpc"
)

type Server struct {
	*grpc.Server
	Port int
	// ETCD 服务注册租约 TTL
	EtcdTTL     int64
	EtcdClient  *clientv3.Client
	etcdManager endpoints.Manager
	etcdKey     string
	cancel      func()
	Name        string
	L           *elog.Component
}

// Serve 启动服务器并且阻塞
func (s *Server) Serve() error {
	// 初始化一个控制整个过程的 ctx
	// 你也可以考虑让外面传进来，这样的话就是 main 函数自己去控制了
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	port := strconv.Itoa(s.Port)

	s.L.Info("正在监听gRPC端口", elog.String("port", port))
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		s.L.Error("gRPC端口监听失败", elog.FieldErr(err), elog.String("port", port))
		return err
	}
	s.L.Info("gRPC端口监听成功", elog.String("port", port))

	// 要先确保启动成功，再注册服务
	s.L.Info("开始注册服务到etcd")
	err = s.register(ctx, port)
	if err != nil {
		s.L.Error("服务注册失败", elog.FieldErr(err))
		return err
	}

	s.L.Info("gRPC服务器开始提供服务", elog.String("addr", ":"+port))
	return s.Server.Serve(l)
}

func (s *Server) register(ctx context.Context, port string) error {
	cli := s.EtcdClient
	serviceName := "service/" + s.Name
	em, err := endpoints.NewManager(cli,
		serviceName)
	if err != nil {
		return err
	}
	s.etcdManager = em
	ip := netx.GetOutboundIP()
	s.etcdKey = serviceName + "/" + ip
	addr := ip + ":" + port
	leaseResp, err := cli.Grant(ctx, s.EtcdTTL)
	if err != nil {
		s.L.Error("创建etcd租约失败", elog.FieldErr(err))
		return err
	}
	s.L.Info("创建etcd租约成功", elog.Int64("lease_id", int64(leaseResp.ID)), elog.Int64("ttl", s.EtcdTTL))

	// 开启续约
	ch, err := cli.KeepAlive(ctx, leaseResp.ID)
	if err != nil {
		s.L.Error("启动etcd续约失败", elog.FieldErr(err))
		return err
	}
	s.L.Info("启动etcd续约成功")
	go func() {
		// 可以预期，当我们的 cancel 被调用的时候，就会退出这个循环
		for chResp := range ch {
			s.L.Debug("续约：", elog.String("resp", chResp.String()))
		}
	}()

	// metadata 我们这里没啥要提供的
	s.L.Info("正在注册服务到etcd", elog.String("key", s.etcdKey), elog.String("addr", addr))
	err = em.AddEndpoint(ctx, s.etcdKey,
		endpoints.Endpoint{Addr: addr}, clientv3.WithLease(leaseResp.ID))
	if err != nil {
		s.L.Error("服务注册到etcd失败", elog.FieldErr(err))
		return err
	}
	s.L.Info("服务注册到etcd成功", elog.String("service_name", serviceName), elog.String("endpoint", addr))
	return nil
}

func (s *Server) Close() error {
	s.cancel()
	if s.etcdManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := s.etcdManager.DeleteEndpoint(ctx, s.etcdKey)
		if err != nil {
			return err
		}
	}
	err := s.EtcdClient.Close()
	if err != nil {
		return err
	}
	s.Server.GracefulStop()
	return nil
}
