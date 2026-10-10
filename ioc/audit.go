package ioc

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/audit"
	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
)

// InitAuditModule 初始化审计模块
func InitAuditModule(db *mongox.Mongo) *audit.Module {
	return audit.NewModule(db)
}

// auditDAOSink 把中间件的领域无关 AuditEntry 翻译为 audit 领域持久化模型，
// 并经 DAO 落袋。领域翻译集中在组合根，使 internal/shared/middleware 无需
// import internal/audit（depcheck R2：shared 保持叶子）。
type auditDAOSink struct {
	dao dao.AuditLogDAO
}

func (s auditDAOSink) Write(ctx context.Context, e middleware.AuditEntry) error {
	_, err := s.dao.Create(ctx, domain.AuditLog{
		OperationType: domain.AuditOperationType(e.OperationType),
		OperatorID:    e.OperatorID,
		OperatorName:  e.OperatorName,
		TenantID:      e.TenantID,
		HTTPMethod:    e.HTTPMethod,
		APIPath:       e.APIPath,
		RequestBody:   e.RequestBody,
		StatusCode:    e.StatusCode,
		Result:        domain.AuditResult(e.Result),
		RequestID:     e.RequestID,
		DurationMs:    e.DurationMs,
		ClientIP:      e.ClientIP,
		UserAgent:     e.UserAgent,
		Ctime:         e.Ctime,
	})
	return err
}

// InitAuditMiddleware 初始化 API 审计中间件
func InitAuditMiddleware(auditModule *audit.Module) *middleware.AuditMiddleware {
	return middleware.NewAuditMiddleware(auditDAOSink{dao: auditModule.AuditDAO}, elog.DefaultLogger)
}

// WireChangeTracker 将审计模块的变更追踪器注入 CAM 资产同步执行器
// （同步收敛 Phase 2 S3a：替代已删除的 asset_sync 死服务注入，恢复同步变更审计）。
func WireChangeTracker(camModule *cam.Module, auditModule *audit.Module) {
	if camModule.TaskModule == nil || auditModule.ChangeTracker == nil {
		return
	}
	camModule.TaskModule.SetChangeTracker(auditModule.ChangeTracker)
}
