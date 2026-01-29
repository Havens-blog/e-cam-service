package ioc

import (
	"time"

	_ "github.com/Havens-blog/e-cam-service/docs" // 导入生成的文档
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cmdb"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func InitWebServer(sp session.Provider, mdls []gin.HandlerFunc, endpointHdl *endpoint.Handler, camModule *cam.Module, cmdbModule *cmdb.Module) *gin.Engine {
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
	camModule.Hdl.PrivateRoutes(server)

	// 注册实例路由
	logger.Info("注册实例路由")
	camGroup := server.Group("/api/v1/cam")
	if camModule.InstanceHdl != nil {
		camModule.InstanceHdl.RegisterRoutes(camGroup)
	}

	// 注册任务路由
	logger.Info("注册任务路由")
	camModule.TaskHdl.RegisterRoutes(camGroup)

	// 注册IAM路由
	if camModule.IAMModule != nil {
		logger.Info("注册IAM路由")
		camModule.IAMModule.RegisterRoutes(server)
		logger.Info("IAM路由注册完成")
	} else {
		logger.Warn("IAM模块未初始化，跳过IAM路由注册")
	}

	// 注册服务树路由
	if camModule.ServiceTreeModule != nil {
		logger.Info("注册服务树路由")
		stGroup := server.Group("/api/v1/cam/service-tree")
		camModule.ServiceTreeModule.RegisterRoutes(stGroup)
		logger.Info("服务树路由注册完成")
	} else {
		logger.Warn("服务树模块未初始化，跳过服务树路由注册")
	}

	// 注册CMDB路由
	logger.Info("注册CMDB路由")
	cmdbGroup := server.Group("/api/v1")
	cmdbModule.RegisterRoutes(cmdbGroup)
	logger.Info("CMDB路由注册完成")

	// 注册 Swagger 文档路由
	logger.Info("注册 Swagger 文档路由")
	server.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	server.GET("/docs", func(c *gin.Context) {
		c.Redirect(302, "/swagger/index.html")
	})

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
