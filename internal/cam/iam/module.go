package iam

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/iam/web"
	"github.com/gin-gonic/gin"
)

// Module IAM模块
type Module struct {
	UserHandler     *web.UserHandler
	GroupHandler    *web.GroupHandler
	SyncHandler     *web.SyncHandler
	AuditHandler    *web.AuditHandler
	TemplateHandler *web.TemplateHandler
}

// RegisterRoutes 注册IAM模块的所有路由
func (m *Module) RegisterRoutes(r *gin.Engine) {
	// 创建IAM路由组
	iamGroup := r.Group("/api/v1/cam/iam")
	{
		// 注册用户管理路由
		m.UserHandler.RegisterRoutes(iamGroup)

		// 注册权限组管理路由
		m.GroupHandler.RegisterRoutes(iamGroup)

		// 注册同步任务管理路由
		m.SyncHandler.RegisterRoutes(iamGroup)

		// 注册审计日志管理路由
		m.AuditHandler.RegisterRoutes(iamGroup)

		// 注册策略模板管理路由
		m.TemplateHandler.RegisterRoutes(iamGroup)
	}
}
