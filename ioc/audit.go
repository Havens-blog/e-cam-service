package ioc

import (
	"github.com/Havens-blog/e-cam-service/internal/audit"
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

// 注：原 WireChangeTracker 将 ChangeTracker 注入死服务 asset_sync（SyncAssets
// 等零调用者），随同步收敛 Phase 2（96c94d3 之后）删除。资产同步的变更审计若需恢复，
// 应在 executor（internal/cam/task/executor）侧实现，见 docs/proposals/sync-consolidation-phase2 S3a。
