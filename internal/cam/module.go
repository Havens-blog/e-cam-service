package cam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/iam"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/gin-gonic/gin"
)

type Module struct {
	Hdl               *Handler
	InstanceHdl       *web.InstanceHandler
	DatabaseHdl       *web.DatabaseHandler // 数据库资源处理器 (旧路由，保留兼容)
	AssetHdl          *web.AssetHandler    // 统一资产处理器 (新RESTful路由)
	Svc               Service
	AccountSvc        CloudAccountService
	ModelSvc          ModelService
	InstanceSvc       service.InstanceService
	TaskModule        *task.Module
	TaskSvc           taskservice.TaskService
	TaskHdl           *taskweb.TaskHandler
	IAMModule         *iam.Module                  // 手动初始化
	ServiceTreeModule *servicetree.Module          // 服务树模块
	AutoScheduler     *scheduler.AutoSyncScheduler // 自动同步调度器
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

	// 注册统一资产路由 (新RESTful风格)
	if m.AssetHdl != nil {
		m.AssetHdl.RegisterRoutes(camGroup)
	}

	// 注册IAM路由
	if m.IAMModule != nil {
		m.IAMModule.RegisterRoutes(r)
	}

	// 注册服务树路由
	if m.ServiceTreeModule != nil {
		m.ServiceTreeModule.RegisterRoutes(camGroup)
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
