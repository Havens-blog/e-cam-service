package ioc

import (
	"context"
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/audit"
	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam"
	"github.com/Havens-blog/e-cam-service/internal/logquery"
	logqueryweb "github.com/Havens-blog/e-cam-service/internal/logquery/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/auditlog"
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

// unifiedAuditSink 双写组合：旧 audit 领域 AuditLog（向后兼容，audit/logs 页
// 继续可用）+ e-common-go/auditlog 统一事件（跨域统一检索的写入侧）。
//
// 双写而非迁移：统一集合是新事物，旧存储/旧前端零改动即回退；待统一检索页
// 稳定后再考虑旧页退役。
type unifiedAuditSink struct {
	legacy   middleware.AuditSink
	writer   auditlog.Writer
	renderAs func(e middleware.AuditEntry) auditlog.AuditEvent
}

func (s unifiedAuditSink) Write(ctx context.Context, e middleware.AuditEntry) error {
	// 1) 旧存储：失败即返回（中间件已异步记日志，不阻塞业务）。
	if err := s.legacy.Write(ctx, e); err != nil {
		return err
	}
	// 2) 统一事件：翻译/写入失败同样仅被中间件记日志（审计是旁路）。
	ue := s.renderAs(e)
	return s.writer.Emit(ctx, &ue)
}

// renderAuditEntryToEvent 把领域无关 AuditEntry 翻译为统一审计事件。
// 映射口径：
//   - ActorType=human：中间件只拦截登录会话里的写操作（GET/OPTIONS/HEAD 直通）；
//   - Action 从 HTTP method 派生（与 OperationType 尾段语义一致）；
//   - 旧字段 http_method/api_path/request_body/request_id/operation_type
//     收进 Detail（统一 envelope 不为 API 级审计单开列）。
func renderAuditEntryToEvent(e middleware.AuditEntry) auditlog.AuditEvent {
	return auditlog.AuditEvent{
		ActorType:    auditlog.ActorTypeHuman,
		ActorID:      e.OperatorID,
		ActorName:    e.OperatorName,
		At:           e.Ctime,
		TenantID:     e.TenantID,
		ClientIP:     e.ClientIP,
		UserAgent:    e.UserAgent,
		Service:      auditlog.ServiceCAM,
		Domain:       auditlog.DomainAudit,
		Action:       actionFromHTTPMethod(e.HTTPMethod),
		ResourceType: "api",
		ResourceID:   e.APIPath,
		Result:       auditlog.Result(e.Result),
		StatusCode:   e.StatusCode,
		DurationMs:   e.DurationMs,
		Detail: map[string]any{
			"http_method":    e.HTTPMethod,
			"api_path":       e.APIPath,
			"request_body":   e.RequestBody,
			"request_id":     e.RequestID,
			"operation_type": e.OperationType,
		},
	}
}

// actionFromHTTPMethod 与 shared/middleware.actionOf 的 method 派生保持同口径
//（sync 尾段特判已体现在 OperationType，统一 Action 层用粗粒度四类）。
func actionFromHTTPMethod(method string) string {
	switch method {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "generic"
	}
}

// logQueryAuditSink 把 logquery 域的结构化检索审计翻译为 audit 领域模型,
// 并经 DAO 落袋。request_body 使用 logquery 侧脱敏后的范围摘要(不含关键字)。
type logQueryAuditSink struct {
	dao dao.AuditLogDAO
}

func (s logQueryAuditSink) WriteLogQuery(ctx context.Context, e logqueryweb.LogQueryAudit) error {
	result := domain.AuditResultFailed
	if e.Success {
		result = domain.AuditResultSuccess
		if e.Partial {
			result = domain.AuditResultPartial
		}
	}
	status := http.StatusBadRequest
	if e.Success {
		status = http.StatusOK
	}

	opType := domain.AuditOpAPILogSearch
	apiPath := "/api/v1/cam/logs/search"
	if e.Operation == "export" {
		opType = domain.AuditOpAPILogExport
		apiPath = "/api/v1/cam/logs/export-audit"
	}

	_, err := s.dao.Create(ctx, domain.AuditLog{
		OperationType: opType,
		OperatorID:    e.OperatorID,
		OperatorName:  e.OperatorName,
		TenantID:      e.TenantID,
		HTTPMethod:    http.MethodPost,
		APIPath:       apiPath,
		RequestBody:   e.Summary(),
		StatusCode:    status,
		Result:        result,
		RequestID:     e.RequestID,
		DurationMs:    e.DurationMs,
		ClientIP:      e.ClientIP,
		UserAgent:     e.UserAgent,
		Ctime:         e.Ctime,
	})
	return err
}

// InitAuditMiddleware 初始化 API 审计中间件（双写：旧 AuditLog + 统一事件）。
func InitAuditMiddleware(auditModule *audit.Module) *middleware.AuditMiddleware {
	legacy := auditDAOSink{dao: auditModule.AuditDAO}
	composite := unifiedAuditSink{
		legacy:   legacy,
		writer:   auditModule.EventWriter,
		renderAs: renderAuditEntryToEvent,
	}
	return middleware.NewAuditMiddleware(composite, elog.DefaultLogger)
}

// WireLogQueryAudit 把日志查询模块接到审计 DAO 上(结构化检索审计)。
// auditModule 为 nil 时 no-op(测试/降级路径下不落审计,查询仍可用)。
func WireLogQueryAudit(logQueryModule *logquery.Module, auditModule *audit.Module) {
	if logQueryModule == nil || logQueryModule.Hdl == nil || auditModule == nil {
		return
	}
	logQueryModule.Hdl.SetAuditSink(logQueryAuditSink{dao: auditModule.AuditDAO})
}

// WireChangeTracker 将审计模块的变更追踪器注入 CAM 资产同步执行器
// （同步收敛 Phase 2 S3a：替代已删除的 asset_sync 死服务注入，恢复同步变更审计）。
func WireChangeTracker(camModule *cam.Module, auditModule *audit.Module) {
	if camModule.TaskModule == nil || auditModule.ChangeTracker == nil {
		return
	}
	camModule.TaskModule.SetChangeTracker(auditModule.ChangeTracker)
}
