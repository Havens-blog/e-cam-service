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
	// DNSRecordReadPort DNS 记录只读端口（供 cert probe 模块枚举拨测目标；
	// nil=未初始化，cert 探测回退台账 SAN 路径）。
	DNSRecordReadPort dns.RecordReadPort

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
