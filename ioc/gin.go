package ioc

import (
	"time"

	endpointv1 "github.com/Duke1616/ecmdb/api/proto/gen/ecmdb/endpoint/v1"
	_ "github.com/Havens-blog/e-cam-service/docs" // 导入生成的文档
	"github.com/Havens-blog/e-cam-service/internal/alert"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cmdb"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func InitWebServer(sp session.Provider, mdls []gin.HandlerFunc, checkPolicy *middleware.CheckPolicyMiddleware, endpointClient endpointv1.EndpointServiceClient, endpointHdl *endpoint.Handler, camModule *cam.Module, cmdbModule *cmdb.Module, alertModule *alert.Module) *gin.Engine {
	logger := elog.DefaultLogger
	logger.Info("开始初始化Web服务器")
	session.SetDefaultProvider(sp)
	gin.SetMode(gin.ReleaseMode)
	server := gin.Default()

	// 添加CORS中间件（最先）
	logger.Info("配置CORS中间件")
	server.Use(corsHdl())

	// 添加基础中间件
	server.Use(mdls...)

	// Swagger 文档路由（不需要认证）
	logger.Info("注册 Swagger 文档路由")
	server.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	server.GET("/docs", func(c *gin.Context) {
		c.Redirect(302, "/swagger/index.html")
	})

	// ===== 以下路由需要 ecmdb session 认证 =====
	server.Use(middleware.EcmdbAuthMiddleware(sp, logger))

	// 租户中间件：从 session 或 header 中提取租户信息
	server.Use(middleware.TenantMiddleware(logger))

	// ecmdb 策略检查中间件
	server.Use(checkPolicy.Build())

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

	// 注册数据库资源路由 (RDS, Redis, MongoDB) - 旧路由，保留兼容
	if camModule.DatabaseHdl != nil {
		logger.Info("注册数据库资源路由")
		camModule.DatabaseHdl.RegisterRoutes(camGroup)
	}

	// 注册统一资产路由 (新RESTful风格)
	if camModule.AssetHdl != nil {
		logger.Info("注册统一资产路由")
		camModule.AssetHdl.RegisterRoutes(camGroup)
	}

	// 注册仪表盘路由
	if camModule.DashboardHdl != nil {
		logger.Info("注册仪表盘路由")
		camModule.DashboardHdl.RegisterRoutesWithGroup(camGroup.Group("/dashboard"))
		logger.Info("仪表盘路由注册完成")
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

	// 注册CMDB路由（独立路由前缀 /api/v1/cmdb）
	logger.Info("注册CMDB路由")
	cmdbGroup := server.Group("/api/v1/cmdb")
	cmdbModule.RegisterRoutes(cmdbGroup)
	logger.Info("CMDB路由注册完成")

	// 注册告警模块路由
	if alertModule != nil {
		logger.Info("注册告警模块路由")
		alertModule.RegisterRoutes(server)
		alertModule.StartEventProcessor(30 * time.Second)
		logger.Info("告警模块路由注册完成")
	} else {
		logger.Warn("告警模块未初始化，跳过告警路由注册")
	}

	// 注册 Swagger 文档路由（已在上方注册，此处跳过）

	// 启动时将 e-cam-service 的路由注册到 ecmdb 的权限系统
	go middleware.RegisterEndpointsToEcmdb(server, endpointClient, logger)

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
		AllowOriginFunc: func(origin string) bool {
			// 开发环境允许所有来源，生产环境应限制为具体域名
			return true
		},
		AllowMethods:  []string{"POST", "GET", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:  []string{"Content-Type", "Authorization", "X-Tenant-ID", "X-Finder-Id", "X-Finder-ID"},
		ExposeHeaders: []string{"X-Access-Token"},
		// 允许携带 cookie 和 Authorization header
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
