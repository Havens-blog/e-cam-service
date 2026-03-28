package cam

import (
	"context"

	costhandler "github.com/Havens-blog/e-cam-service/internal/cam/cost/handler"
	"github.com/Havens-blog/e-cam-service/internal/cam/dictionary"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/template"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type Module struct {
	Hdl               *Handler
	InstanceHdl       *web.InstanceHandler
	DatabaseHdl       *web.DatabaseHandler  // 数据库资源处理器 (旧路由，保留兼容)
	AssetHdl          *web.AssetHandler     // 统一资产处理器 (新RESTful路由)
	DashboardHdl      *web.DashboardHandler // 仪表盘处理器
	Svc               Service
	AccountSvc        CloudAccountService
	ModelSvc          ModelService
	InstanceSvc       service.InstanceService
	AssetSyncSvc      service.AssetSyncService // 资产同步服务 (同步到CMDB)
	TaskModule        *task.Module
	TaskSvc           taskservice.TaskService
	TaskHdl           *taskweb.TaskHandler
	IAMModule         *iam.Module                  // 使用独立的 IAM 模块
	ServiceTreeModule *servicetree.Module          // 服务树模块
	AutoScheduler     *scheduler.AutoSyncScheduler // 自动同步调度器
	Logger            *elog.Component              // 日志组件

	// 成本管理模块处理器
	CostHdl       *costhandler.CostHandler       // 成本分析处理器
	BudgetHdl     *costhandler.BudgetHandler     // 预算管理处理器
	AllocationHdl *costhandler.AllocationHandler // 成本分摊处理器
	CollectorHdl  *costhandler.CollectorHandler  // 采集管理处理器

	// 数据字典模块处理器
	DictHdl *dictionary.DictHandler

	// 主机模板模块处理器
	TemplateHdl *template.TemplateHandler

	// 成本管理模块服务（供定时任务使用）
	CostCollectorSvc CostCollectorService
	CostBudgetSvc    CostBudgetService
	CostAnomalySvc   CostAnomalyService
	CostOptimizerSvc CostOptimizerService
}

// CostCollectorService 采集服务接口（供定时任务使用）
type CostCollectorService interface {
	StartScheduledCollection(ctx context.Context) error
}

// CostBudgetService 预算检查服务接口（供定时任务使用）
type CostBudgetService interface {
	CheckBudgets(ctx context.Context, tenantID string) error
}

// CostAnomalyService 异常检测服务接口（供定时任务使用）
type CostAnomalyService interface {
	DetectAnomalies(ctx context.Context, tenantID, date string) error
}

// CostOptimizerService 优化建议服务接口（供定时任务使用）
type CostOptimizerService interface {
	GenerateRecommendations(ctx context.Context, tenantID string) error
}

// RegisterRoutes 注册所有路由
func (m *Module) RegisterRoutes(r *gin.Engine) {
	camGroup := r.Group("/api/v1/cam")

	// 注册实例路由
	if m.InstanceHdl != nil {
		m.InstanceHdl.RegisterRoutes(camGroup)
	}

	// 注册数据库资源路由 (RDS, Redis, MongoDB) - 旧路由，保留兼容
	if m.DatabaseHdl != nil {
		m.DatabaseHdl.RegisterRoutes(camGroup)
	}

	// 注册统一资产路由 (新RESTful风格，使用租户中间件)
	if m.AssetHdl != nil {
		assetsGroup := camGroup.Group("/assets")
		assetsGroup.Use(middleware.TenantMiddleware(m.Logger))
		assetsGroup.Use(middleware.RequireTenant(m.Logger))
		m.AssetHdl.RegisterRoutesWithGroup(assetsGroup)
	}

	// 注册仪表盘路由 (使用租户中间件)
	if m.DashboardHdl != nil {
		dashboardGroup := camGroup.Group("/dashboard")
		dashboardGroup.Use(middleware.TenantMiddleware(m.Logger))
		dashboardGroup.Use(middleware.RequireTenant(m.Logger))
		m.DashboardHdl.RegisterRoutesWithGroup(dashboardGroup)
	}

	// 注册IAM路由
	if m.IAMModule != nil {
		m.IAMModule.RegisterRoutes(r)
	}

	// 注册服务树路由
	if m.ServiceTreeModule != nil {
		m.ServiceTreeModule.RegisterRoutes(camGroup)
	}

	// 注册主机模板路由 (使用租户中间件)
	if m.TemplateHdl != nil {
		templateGroup := camGroup.Group("")
		templateGroup.Use(middleware.TenantMiddleware(m.Logger))
		templateGroup.Use(middleware.RequireTenant(m.Logger))
		m.TemplateHdl.RegisterRoutes(templateGroup)
	}
}

// StartScheduler 启动自动同步调度器
func (m *Module) StartScheduler() {
	if m.AutoScheduler != nil {
		m.AutoScheduler.Start()
	}
}

// Stop 停止模块
func (m *Module) Stop() {
	if m.AutoScheduler != nil {
		m.AutoScheduler.Stop()
	}
	if m.TaskModule != nil {
		m.TaskModule.Stop()
	}
}
