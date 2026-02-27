// Package audit 审计模块 - 独立实现
// 提供全链路 API 操作审计和资产变更历史追踪能力
package audit

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/audit/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// Module 审计模块
type Module struct {
	AuditHandler  *web.AuditHandler
	ChangeHandler *web.ChangeHandler
	ChangeTracker *service.ChangeTracker
	AuditDAO      dao.AuditLogDAO // 暴露给审计中间件使用
}

// NewModule 初始化审计模块
func NewModule(db *mongox.Mongo) *Module {
	logger := elog.DefaultLogger

	// 初始化 DAO
	auditDAO := dao.NewAuditLogDAO(db)
	changeDAO := dao.NewChangeRecordDAO(db)

	// 初始化索引
	if err := auditDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("初始化审计日志索引失败", elog.FieldErr(err))
	}
	if err := changeDAO.InitIndexes(context.Background()); err != nil {
		logger.Error("初始化变更记录索引失败", elog.FieldErr(err))
	}

	// 初始化 Service
	auditSvc := service.NewAuditService(auditDAO, logger)
	changeTracker := service.NewChangeTracker(changeDAO, logger)

	// 初始化 Handler
	auditHandler := web.NewAuditHandler(auditSvc, logger)
	changeHandler := web.NewChangeHandler(changeTracker, logger)

	return &Module{
		AuditHandler:  auditHandler,
		ChangeHandler: changeHandler,
		ChangeTracker: changeTracker,
		AuditDAO:      auditDAO,
	}
}

// RegisterRoutes 注册审计模块路由
// 审计日志: /api/v1/cam/audit/logs, /logs/export, /reports
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	m.AuditHandler.RegisterRoutes(r)
}
