// Package audit 审计模块 - 独立实现
// 提供全链路 API 操作审计和资产变更历史追踪能力
package audit

import (
	"context"

	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/audit/web"
)

// Module 审计模块
type Module struct {
	AuditHandler  *web.AuditHandler
	ChangeHandler *web.ChangeHandler
	EventHandler  *web.AuditEventHandler
	ChangeTracker *service.ChangeTracker
	AuditDAO      dao.AuditLogDAO   // 暴露给审计中间件使用（旧操作审计）
	AuditEventDAO dao.AuditEventDAO // 统一审计事件读侧（跨域检索）
	EventWriter   auditlog.Writer   // 统一审计事件写侧（组合根据此组装组合 sink）
}

// NewModule 初始化审计模块
// fed 为联邦读客户端（F2，可为 nil = 未配置联邦，统一检索退化为本侧单源）。
func NewModule(db *mongox.Mongo, fed *service.IAMEventFederationClient) *Module {
	logger := elog.DefaultLogger

	// 初始化 DAO
	auditDAO := dao.NewAuditLogDAO(db)
	changeDAO := dao.NewChangeRecordDAO(db)
	eventDAO := dao.NewAuditEventDAO(db)

	// 初始化索引
	if err := auditDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("初始化审计日志索引失败", elog.FieldErr(err))
	}
	if err := changeDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("初始化变更记录索引失败", elog.FieldErr(err))
	}
	// 统一审计事件：索引含幂等稀疏唯一键，启动期建好（写侧 SDK 不再自建）
	if err := eventDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("初始化统一审计事件索引失败", elog.FieldErr(err))
	}

	// 统一审计事件写入端口：先于服务创建（变更追踪/变更单审计的旁路双写需要）
	eventWriter := auditlog.NewMongoWriter(db.Database())

	// 初始化 Service
	auditSvc := service.NewAuditService(auditDAO, logger)
	changeTracker := service.NewChangeTracker(changeDAO, logger, eventWriter)
	eventSvc := service.NewAuditEventService(eventDAO, logger, fed)

	// 初始化 Handler
	auditHandler := web.NewAuditHandler(auditSvc, logger)
	changeHandler := web.NewChangeHandler(changeTracker, logger)
	eventHandler := web.NewAuditEventHandler(eventSvc, logger)

	return &Module{
		AuditHandler:  auditHandler,
		ChangeHandler: changeHandler,
		EventHandler:  eventHandler,
		ChangeTracker: changeTracker,
		AuditDAO:      auditDAO,
		AuditEventDAO: eventDAO,
		EventWriter:   eventWriter,
	}
}

// RegisterRoutes 注册审计模块路由
// 审计日志: /api/v1/cam/audit/logs, /logs/export, /reports
// 统一审计事件: /api/v1/cam/audit/events
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	m.AuditHandler.RegisterRoutes(r)
	m.EventHandler.RegisterRoutes(r)
}
