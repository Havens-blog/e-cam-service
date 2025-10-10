package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/Havens-blog/e-cam-service/pkg/grpcx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/task/ecron"
)

type App struct {
	Web    *gin.Engine
	Grpc   *grpcx.Server
	Jobs   []*ecron.Component
	EndSvc endpoint.Service
	CamSvc cam.Service
}
