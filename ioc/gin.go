package ioc

import (
	"time"

	endpointv1 "github.com/Havens-blog/e-cam-service/api/proto/gen/ecmdb/endpoint/v1"
	_ "github.com/Havens-blog/e-cam-service/docs" // 导入生成的文档
	"github.com/Havens-blog/e-cam-service/internal/alert"
	"github.com/Havens-blog/e-cam-service/internal/audit"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/cmdb"
	"github.com/Havens-blog/e-cam-service/internal/endpoint"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/internal/topology"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/spf13/viper"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func InitWebServer(sp session.Provider, mdls []gin.HandlerFunc, checkPolicy *middleware.CheckPolicyMiddleware, auditMdl *middleware.AuditMiddleware, auditModule *audit.Module, endpointClient endpointv1.EndpointServiceClient, endpointHdl *endpoint.Handler, camModule *cam.Module, cmdbModule *cmdb.Module, alertModule *alert.Module, db *mongox.Mongo) *gin.Engine {
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

	// 请求ID中间件（在认证之前）
	server.Use(middleware.RequestIDMiddleware())

	// Swagger 文档路由（不需要认证）
	logger.Info("注册 Swagger 文档路由")
	server.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	server.GET("/docs", func(c *gin.Context) {
		c.Redirect(302, "/swagger/index.html")
	})

	// 健康检查路由（不需要认证）
	server.GET("/api/v1/cam/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0, "msg": "ok"})
	})

	// ===== 以下路由需要 ecmdb session 认证 =====
	// 加固版认证中间件，支持白名单
	var authCfg middleware.AuthConfig
	_ = viper.UnmarshalKey("auth", &authCfg)
	server.Use(middleware.EcmdbAuthMiddlewareWithConfig(sp, authCfg, logger))

	// 租户由认证中间件从 JWT claims 解析并写入上下文（见 middleware.TenantIDKey），
	// 不再有独立的租户中间件：客户端自报的租户一律不采纳。

	// ecmdb 策略检查中间件（加固版）
	server.Use(checkPolicy.Build())

	// API 操作审计中间件
	server.Use(auditMdl.Build())

	// 注册路由
	logger.Info("注册路由")
	endpointHdl.PrivateRoutes(server)
	camModule.Hdl.PrivateRoutes(server)

	// 注册实例路由
	logger.Info("注册实例路由")
	camGroup := server.Group("/api/v1/cam")

	// tenantScoped 返回一个挂了 RequireTenant 的 camGroup 子组。
	//
	// 为什么必须在这里挂，而不是在 internal/cam/module.go：
	// cam.Module.RegisterRoutes 虽然也写了同样的拦截，但它**没有任何调用方**
	// （全仓 grep `camModule.RegisterRoutes` 为 0）；线上生效的注册路径是
	// ioc/wire_gen.go:42 → 本函数。写在那边的 RequireTenant 一律不生效。
	//
	// 为什么这些组需要拦截：这些 handler 全部以 middleware.GetTenantID(ctx) 构造
	// filter，而多数 filter 的 DAO 保留了「if filter.TenantID != 0 才加租户谓词」
	// 的可选语义（该可选性是机器路径枚举云账号所必需，见报告）。若放行
	// tenant_id=0 的会话（eiam 用 0 表示「等待选择租户」的临时凭证），租户谓词会被
	// 整体丢弃，查询将返回**全部租户**的数据。按设计 §4.3，0 必须在中间件边界拒绝，
	// 不得进入任何 DAO。
	tenantScoped := func() *gin.RouterGroup {
		g := camGroup.Group("")
		g.Use(middleware.RequireTenant(logger))
		return g
	}

	if camModule.InstanceHdl != nil {
		camModule.InstanceHdl.RegisterRoutes(tenantScoped())
	}

	// 注册数据库资源路由 (RDS, Redis, MongoDB) - 旧路由，保留兼容
	if camModule.DatabaseHdl != nil {
		logger.Info("注册数据库资源路由")
		camModule.DatabaseHdl.RegisterRoutes(tenantScoped())
	}

	// 注册统一资产路由 (新RESTful风格)
	if camModule.AssetHdl != nil {
		logger.Info("注册统一资产路由")
		camModule.AssetHdl.RegisterRoutes(tenantScoped())
	}

	// 注册仪表盘路由
	if camModule.DashboardHdl != nil {
		logger.Info("注册仪表盘路由")
		camModule.DashboardHdl.RegisterRoutesWithGroup(tenantScoped().Group("/dashboard"))
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
		// servicetree 的 handler 内已有 6 处 `if tenantID == 0` 判空，但并非每个端点都有；
		// 组级 RequireTenant 补齐其余端点并把拒绝点前移到边界，二者不冲突
		// （中间件先返回 403，handler 内的判空成为不可达兜底）。
		stGroup := server.Group("/api/v1/cam/service-tree")
		stGroup.Use(middleware.RequireTenant(logger))
		camModule.ServiceTreeModule.RegisterRoutes(stGroup)
		logger.Info("服务树路由注册完成")
	} else {
		logger.Warn("服务树模块未初始化，跳过服务树路由注册")
	}

	// 注册成本管理路由
	if camModule.CostHdl != nil {
		logger.Info("注册成本分析路由")
		camModule.CostHdl.PrivateRoutes(server)
	}
	if camModule.BudgetHdl != nil {
		logger.Info("注册预算管理路由")
		camModule.BudgetHdl.PrivateRoutes(server)
	}
	if camModule.AllocationHdl != nil {
		logger.Info("注册成本分摊路由")
		camModule.AllocationHdl.PrivateRoutes(server)
	}
	if camModule.CollectorHdl != nil {
		logger.Info("注册采集管理路由")
		camModule.CollectorHdl.PrivateRoutes(server)
	}

	// 注册数据字典路由
	if camModule.DictHdl != nil {
		logger.Info("注册数据字典路由")
		camModule.DictHdl.RegisterRoutes(tenantScoped())
		logger.Info("数据字典路由注册完成")
	}

	// 注册主机模板路由
	if camModule.TemplateHdl != nil {
		logger.Info("注册主机模板路由")
		camModule.TemplateHdl.RegisterRoutes(tenantScoped())
		logger.Info("主机模板路由注册完成")
	}

	// 注册标签管理路由
	if camModule.TagHdl != nil {
		logger.Info("注册标签管理路由")
		camModule.TagHdl.RegisterRoutes(tenantScoped())
		logger.Info("标签管理路由注册完成")
	}

	// 注册 DNS 管理路由
	if camModule.DNSHdl != nil {
		logger.Info("注册 DNS 管理路由")
		camModule.DNSHdl.RegisterRoutes(tenantScoped())
		logger.Info("DNS 管理路由注册完成")
	}

	// 注册CMDB路由（挂在 /api/v1/cam 下，前端请求 /api/v1/cam/cmdb/...）
	logger.Info("注册CMDB路由")
	cmdbModule.RegisterRoutes(camGroup)
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

	// 注册拓扑模块路由
	topoModule := topology.NewModule(db)
	logger.Info("注册拓扑模块路由")
	topoModule.RegisterRoutes(server)
	logger.Info("拓扑模块路由注册完成")

	// 注册审计模块路由
	if auditModule != nil {
		logger.Info("注册审计模块路由")
		// 审计组可以整组挂守卫：组内 5 条路由（/logs、/logs/export、/reports 与
		// 下面的 /changes、/changes/summary）**全部**消费会话租户，没有免租户端点。
		// 其 DAO（audit/repository/dao/audit.go:209、change.go:144）是
		// `if filter.TenantID != 0`，租户为 0 时谓词被丢弃 → 会返回全部租户的
		// 审计日志与变更历史。按设计 §4.3 在边界拒绝。
		auditGroup := server.Group("/api/v1/cam/audit")
		auditGroup.Use(middleware.RequireTenant(logger))
		auditModule.RegisterRoutes(auditGroup)

		// 变更历史路由（挂在 audit 下，避免与 /assets/:id 路由冲突）
		if auditModule.ChangeHandler != nil {
			auditGroup.GET("/changes", auditModule.ChangeHandler.ListAssetChanges)
			auditGroup.GET("/changes/summary", auditModule.ChangeHandler.GetChangeSummary)
		}
		logger.Info("审计模块路由注册完成")
	}

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
		AllowHeaders:  []string{"Content-Type", "Authorization", "X-Finder-Id", "X-Finder-ID", "X-Request-ID"},
		ExposeHeaders: []string{"X-Access-Token", "X-Request-ID", "X-Request-User"},
		// 允许携带 cookie 和 Authorization header
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
