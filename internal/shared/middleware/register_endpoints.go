package middleware

import (
	"context"
	"strings"
	"time"

	endpointv1 "github.com/Havens-blog/e-cam-service/api/proto/gen/ecmdb/endpoint/v1"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// RegisterEndpointsToEcmdb 将 e-cam-service 的路由注册到 ecmdb 的 endpoint 系统
// 这样 ecmdb 的 Casbin 策略引擎就能管理 e-cam-service 的 API 权限
//
// 资源域拆分（任务 7.2）：/api/v1/cam → CAM 域（既有）；/api/v1/certs →
// CERT 域（证书管理功能域 EIAM 资源声明，独立 BatchRegister 调用）。三角色
// （运维工程师/运维主管+审计/只读查看者）→能力码（cert:manage/cert:settings，
// 见 internal/cert/web/role_middleware.go）→物理端点的绑定关系在 eiam 侧
// 管理；本注册为启动期端点清单同步（沿用既有 EIAM 同步机制）。
// newEndpoint 构造单条端点声明（CAM/CERT 域共用；IsAuth/IsPermission 恒真——
// e-cam-service 端点均经认证与授权）。
func newEndpoint(path, method, desc, resource string) *endpointv1.Endpoint {
	return &endpointv1.Endpoint{
		Path:         path,
		Method:       method,
		Desc:         desc,
		Resource:     resource,
		IsAuth:       true,
		IsPermission: true,
	}
}

func RegisterEndpointsToEcmdb(engine *gin.Engine, client endpointv1.EndpointServiceClient, logger *elog.Component) {
	if client == nil {
		logger.Warn("ecmdb endpoint client 未初始化，跳过端点注册")
		return
	}

	routes := engine.Routes()
	camEndpoints := make([]*endpointv1.Endpoint, 0)
	certEndpoints := make([]*endpointv1.Endpoint, 0)

	for _, route := range routes {
		// 只注册 /api/v1/cam 开头的路由（e-cam-service 的业务路由）
		if len(route.Path) > 11 && route.Path[:12] == "/api/v1/cam/" {
			camEndpoints = append(camEndpoints, newEndpoint(route.Path, route.Method,
				"e-cam-service: "+route.Path, "CAM"))
			continue
		}
		// 证书管理功能域（/api/v1/certs 前缀，含 /api/v1/certs 本身）
		if strings.HasPrefix(route.Path, "/api/v1/certs") {
			certEndpoints = append(certEndpoints, newEndpoint(route.Path, route.Method,
				"e-cam-service cert: "+route.Method+" "+route.Path, "CERT"))
		}
	}

	if len(camEndpoints) == 0 && len(certEndpoints) == 0 {
		logger.Info("没有需要注册的端点")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, batch := range []struct {
		resource  string
		endpoints []*endpointv1.Endpoint
	}{
		{resource: "CAM", endpoints: camEndpoints},
		{resource: "CERT", endpoints: certEndpoints},
	} {
		if len(batch.endpoints) == 0 {
			continue
		}
		_, err := client.BatchRegister(ctx, &endpointv1.BatchRegisterEndpointsReq{
			Resource:  batch.resource,
			Endpoints: batch.endpoints,
		})
		if err != nil {
			logger.Error("注册端点到 ecmdb 失败",
				elog.FieldErr(err),
				elog.String("resource", batch.resource),
				elog.Int("count", len(batch.endpoints)))
			continue
		}
		logger.Info("成功注册端点到 ecmdb",
			elog.String("resource", batch.resource),
			elog.Int("count", len(batch.endpoints)))
	}
}
