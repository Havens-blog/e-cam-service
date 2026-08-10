package cam

import (
	"context"

	costhandler "github.com/Havens-blog/e-cam-service/internal/cam/cost/handler"
	"github.com/Havens-blog/e-cam-service/internal/cam/dictionary"
	"github.com/Havens-blog/e-cam-service/internal/cam/dns"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree"
	"github.com/Havens-blog/e-cam-service/internal/cam/tag"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/template"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
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

	// 标签管理模块处理器
	TagHdl *tag.TagHandler

	// DNS 管理模块处理器
	DNSHdl *dns.DNSHandler

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
	CheckBudgets(ctx context.Context, tenantID int64) error
}

// CostAnomalyService 异常检测服务接口（供定时任务使用）
type CostAnomalyService interface {
	DetectAnomalies(ctx context.Context, tenantID int64, date string) error
}

// CostOptimizerService 优化建议服务接口（供定时任务使用）
type CostOptimizerService interface {
	GenerateRecommendations(ctx context.Context, tenantID int64) error
}

// RegisterRoutes 注册所有路由
//
// ⚠️ 本方法当前**没有任何调用方**（全仓 grep `camModule.RegisterRoutes` 为 0）。
// 线上真正生效的注册路径是 ioc/wire_gen.go:42 → ioc.InitWebServer（ioc/gin.go:76），
// 那里自建 camGroup 并直接注册各 Hdl。故本方法内的 RequireTenant 全部**不生效**。
// 修改租户边界请改 ioc/gin.go；此处保留是为了让该方法自身保持一致，
// 一旦将来被接线即具备正确的边界语义。
func (m *Module) RegisterRoutes(r *gin.Engine) {
	camGroup := r.Group("/api/v1/cam")

	// 注册实例路由
	//
	// instance_handler 以 middleware.GetTenantID(ctx) 构造 InstanceFilter/SearchFilter，
	// 而这两个 filter 的 DAO 保留了「if filter.TenantID != 0 才加租户谓词」的可选语义。
	// 若放行 tenant_id=0 的会话（eiam 用 0 表示「等待选择租户」的临时凭证），
	// 租户谓词会被整体丢弃，List 会返回全部租户的实例。
	// 按设计 §4.3，0 必须在中间件边界拒绝，不得进入任何 DAO。
	// 注意：受上面的方法级说明所限，此处的拦截当前不生效。
	if m.InstanceHdl != nil {
		instanceGroup := camGroup.Group("")
		instanceGroup.Use(middleware.RequireTenant(m.Logger))
		m.InstanceHdl.RegisterRoutes(instanceGroup)
	}

	// 注册数据库资源路由 (RDS, Redis, MongoDB) - 旧路由，保留兼容
	// 同上：database_handler 也用会话租户构造 InstanceFilter。
	if m.DatabaseHdl != nil {
		databaseGroup := camGroup.Group("")
		databaseGroup.Use(middleware.RequireTenant(m.Logger))
		m.DatabaseHdl.RegisterRoutes(databaseGroup)
	}

	// 注册统一资产路由 (新RESTful风格，使用租户中间件)
	if m.AssetHdl != nil {
		assetsGroup := camGroup.Group("/assets")
		assetsGroup.Use(middleware.RequireTenant(m.Logger))
		m.AssetHdl.RegisterRoutesWithGroup(assetsGroup)
	}

	// 注册仪表盘路由 (使用租户中间件)
	if m.DashboardHdl != nil {
		dashboardGroup := camGroup.Group("/dashboard")
		dashboardGroup.Use(middleware.RequireTenant(m.Logger))
		m.DashboardHdl.RegisterRoutesWithGroup(dashboardGroup)
	}

	// 注册IAM路由
	if m.IAMModule != nil {
		m.IAMModule.RegisterRoutes(r)
	}

	// 注册服务树路由
	//
	// servicetree 的 handler 内已有 6 处 `if tenantID == 0` 判空，但并非每个
	// handler 都有；组级 RequireTenant 补齐其余端点，并把拒绝点前移到边界。
	// 二者不冲突：中间件先返回 403，handler 内的判空成为不可达的兜底。
	if m.ServiceTreeModule != nil {
		serviceTreeGroup := camGroup.Group("")
		serviceTreeGroup.Use(middleware.RequireTenant(m.Logger))
		m.ServiceTreeModule.RegisterRoutes(serviceTreeGroup)
	}

	// 注册主机模板路由 (使用租户中间件)
	if m.TemplateHdl != nil {
		templateGroup := camGroup.Group("")
		templateGroup.Use(middleware.RequireTenant(m.Logger))
		m.TemplateHdl.RegisterRoutes(templateGroup)
	}

	// 注册标签管理路由 (使用租户中间件)
	if m.TagHdl != nil {
		tagGroup := camGroup.Group("")
		tagGroup.Use(middleware.RequireTenant(m.Logger))
		m.TagHdl.RegisterRoutes(tagGroup)
	}

	// 注册 DNS 管理路由 (使用租户中间件)
	if m.DNSHdl != nil {
		dnsGroup := camGroup.Group("")
		dnsGroup.Use(middleware.RequireTenant(m.Logger))
		m.DNSHdl.RegisterRoutes(dnsGroup)
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
