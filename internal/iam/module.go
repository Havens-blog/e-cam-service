// Package iam 提供 IAM（身份和访问管理）功能
// 这是从 internal/cam/iam 重构后的独立模块
package iam

import (
	"github.com/Havens-blog/e-cam-service/internal/iam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// Module IAM模块
type Module struct {
	UserHandler       *web.UserHandler
	GroupHandler      *web.UserGroupHandler
	PermissionHandler *web.PermissionHandler
	SyncHandler       *web.SyncHandler
	AuditHandler      *web.AuditHandler
	TemplateHandler   *web.TemplateHandler
	TenantHandler     *web.TenantHandler
	Logger            *elog.Component
}

// NewModule 创建 IAM 模块
func NewModule(
	userHandler *web.UserHandler,
	groupHandler *web.UserGroupHandler,
	permissionHandler *web.PermissionHandler,
	syncHandler *web.SyncHandler,
	auditHandler *web.AuditHandler,
	templateHandler *web.TemplateHandler,
	tenantHandler *web.TenantHandler,
	logger *elog.Component,
) *Module {
	return &Module{
		UserHandler:       userHandler,
		GroupHandler:      groupHandler,
		PermissionHandler: permissionHandler,
		SyncHandler:       syncHandler,
		AuditHandler:      auditHandler,
		TemplateHandler:   templateHandler,
		TenantHandler:     tenantHandler,
		Logger:            logger,
	}
}

// RegisterRoutes 注册IAM模块的所有路由
func (m *Module) RegisterRoutes(r *gin.Engine) {
	// 创建IAM路由组，应用租户中间件
	iamGroup := r.Group("/api/v1/cam/iam")
	iamGroup.Use(middleware.TenantMiddleware(m.Logger))

	{
		// 租户管理路由（不需要租户ID验证，因为是管理租户本身）
		m.TenantHandler.RegisterRoutes(iamGroup)

		// 需要租户ID的路由组
		tenantRequired := iamGroup.Group("")
		tenantRequired.Use(middleware.RequireTenant(m.Logger))
		{
			// 注册用户管理路由
			m.UserHandler.RegisterRoutes(tenantRequired)

			// 注册用户组管理路由
			m.GroupHandler.RegisterRoutes(tenantRequired)

			// 注册权限管理路由
			m.PermissionHandler.RegisterRoutes(tenantRequired)

			// 注册同步任务管理路由
			m.SyncHandler.RegisterRoutes(tenantRequired)

			// 注册审计日志管理路由
			m.AuditHandler.RegisterRoutes(tenantRequired)

			// 注册策略模板管理路由
			m.TemplateHandler.RegisterRoutes(tenantRequired)
		}
	}
}
