package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/audit"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
)

// InitAuditModule 初始化审计模块
func InitAuditModule(db *mongox.Mongo) *audit.Module {
	return audit.NewModule(db)
}

// InitAuditMiddleware 初始化 API 审计中间件
func InitAuditMiddleware(auditModule *audit.Module) *middleware.AuditMiddleware {
	return middleware.NewAuditMiddleware(auditModule.AuditDAO, elog.DefaultLogger)
}

// WireChangeTracker 将审计模块的变更追踪器注入 CAM 资产同步执行器
// （同步收敛 Phase 2 S3a：替代已删除的 asset_sync 死服务注入，恢复同步变更审计）。
func WireChangeTracker(camModule *cam.Module, auditModule *audit.Module) {
	if camModule.TaskModule == nil || auditModule.ChangeTracker == nil {
		return
	}
	camModule.TaskModule.SetChangeTracker(auditModule.ChangeTracker)
}
