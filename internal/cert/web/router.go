package web

import "github.com/gin-gonic/gin"

// RegisterRoutes 在 /api/v1/certs 前缀下注册证书域路由（2.2 导入 + 2.3 台账 +
// 3.6 引用关系 + 4.5 看板与全局配置 + 5.11 变更管理）。鉴权由全局中间件链
// （ioc/gin.go EcmdbAuthMiddleware）承接，此处不重复挂载；settings 与变更面
// 端点级 RequireRoles 角色门卫（7.2 EIAM 全量接线）。
// reference/dashboard/settings/change 先于 ledger 注册：/reverse、/dashboard、
// /settings、/changes 静态段须先于 /:id 进入路由树。
func RegisterRoutes(
	server *gin.Engine,
	h *CertHandler,
	reference *ReferenceHandler,
	ledger *LedgerHandler,
	dashboard *DashboardHandler,
	settings *SettingsHandler,
	change *ChangeHandler,
) {
	// 组级 EIAM 角色映射中间件（7.2）：claims → RequireRoles 消费的角色 +
	// 操作者注入请求 ctx（审计归因）；测试路由无会话时为 no-op。
	g := server.Group("/api/v1/certs", CertRoleMiddleware())
	h.RegisterRoutes(g)
	reference.RegisterRoutes(g)
	dashboard.RegisterRoutes(g)
	settings.RegisterRoutes(g)
	change.RegisterRoutes(g)
	ledger.RegisterRoutes(g)
}
