package cam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/iam"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/gin-gonic/gin"
)

type Module struct {
	Hdl        *Handler
	Svc        Service
	AccountSvc CloudAccountService
	ModelSvc   ModelService
	TaskModule *task.Module
	TaskSvc    taskservice.TaskService
	TaskHdl    *taskweb.TaskHandler
	IAMModule  *iam.Module // 手动初始化
}

// RegisterRoutes 注册所有路由
func (m *Module) RegisterRoutes(r *gin.Engine) {
	// 注册IAM路由
	if m.IAMModule != nil {
		m.IAMModule.RegisterRoutes(r)
	}
}

// Stop 停止模块
func (m *Module) Stop() {
	if m.TaskModule != nil {
		m.TaskModule.Stop()
	}
}
