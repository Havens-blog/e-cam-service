package cam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
)

type Module struct {
	Hdl        *Handler
	Svc        Service
	AccountSvc CloudAccountService
	ModelSvc   ModelService
	TaskModule *task.Module
	TaskSvc    taskservice.TaskService
	TaskHdl    *taskweb.TaskHandler
}

// Stop 停止模块
func (m *Module) Stop() {
	if m.TaskModule != nil {
		m.TaskModule.Stop()
	}
}
