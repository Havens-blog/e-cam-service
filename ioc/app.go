package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/Havens-blog/e-cam-service/pkg/grpcx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/task/ecron"
	"go.uber.org/zap"
)

type App struct {
	Logger    *zap.Logger        // 日志组件
	Web       *gin.Engine        // Web服务器
	Grpc      *grpcx.Server      // gRPC服务器
	Jobs      []*ecron.Component // 定时任务
	EndModule *endpoint.Module   // Endpoint模块
	CamModule *cam.Module        // CAM模块
}
