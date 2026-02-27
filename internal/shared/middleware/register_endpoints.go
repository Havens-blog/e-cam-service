package middleware

import (
	"context"
	"time"

	endpointv1 "github.com/Duke1616/ecmdb/api/proto/gen/ecmdb/endpoint/v1"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// RegisterEndpointsToEcmdb 将 e-cam-service 的路由注册到 ecmdb 的 endpoint 系统
// 这样 ecmdb 的 Casbin 策略引擎就能管理 e-cam-service 的 API 权限
func RegisterEndpointsToEcmdb(engine *gin.Engine, client endpointv1.EndpointServiceClient, logger *elog.Component) {
	if client == nil {
		logger.Warn("ecmdb endpoint client 未初始化，跳过端点注册")
		return
	}

	routes := engine.Routes()
	var endpoints []*endpointv1.Endpoint

	for _, route := range routes {
		// 只注册 /api/v1/cam 开头的路由（e-cam-service 的业务路由）
		if len(route.Path) > 11 && route.Path[:12] == "/api/v1/cam/" {
			endpoints = append(endpoints, &endpointv1.Endpoint{
				Path:         route.Path,
				Method:       route.Method,
				Desc:         "e-cam-service: " + route.Path,
				Resource:     "CAM",
				IsAuth:       true,
				IsPermission: true,
			})
		}
	}

	if len(endpoints) == 0 {
		logger.Info("没有需要注册的端点")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.BatchRegister(ctx, &endpointv1.BatchRegisterEndpointsReq{
		Resource:  "CAM",
		Endpoints: endpoints,
	})

	if err != nil {
		logger.Error("注册端点到 ecmdb 失败", elog.FieldErr(err), elog.Int("count", len(endpoints)))
		return
	}

	logger.Info("成功注册端点到 ecmdb", elog.Int("count", len(endpoints)))
}
