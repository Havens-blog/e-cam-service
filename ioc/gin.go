package ioc

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

func InitWebServer(sp session.Provider, mdls []gin.HandlerFunc, endpointHdl *endpoint.Handler, camHdl *cam.Handler) *gin.Engine {
	logger := elog.DefaultLogger
	logger.Info("开始初始化Web服务器")
	session.SetDefaultProvider(sp)
	gin.SetMode(gin.ReleaseMode)
	server := gin.Default()

	// 添加会话中间件
	logger.Info("配置会话中间件")
	server.Use(mdls...)

	// 添加CORS中间件
	logger.Info("配置CORS中间件")
	server.Use(corsHdl())

	// 注册路由
	logger.Info("注册路由")
	endpointHdl.PrivateRoutes(server)
	camHdl.PrivateRoutes(server)

	logger.Info("Web服务器初始化完成")
	return server
}

func InitGinMiddlewares() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		corsHdl(),
		func(ctx *gin.Context) {
		},
	}
}

func corsHdl() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"POST", "GET"},
		AllowHeaders:  []string{"Content-Type", "Authorization"},
		ExposeHeaders: []string{"X-Access-Token"},
		// 是否允许你带 cookie 之类的东西
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
