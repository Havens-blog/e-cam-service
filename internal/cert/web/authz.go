package web

import (
	"net/http"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------
// 角色门卫（任务 4.5：settings 面先行拦截；7.2 EIAM 全量接线）
// ---------------------------------------------------------------------

// Role 证书域操作者角色（PRD EIAM 三角色：运维工程师=读写、运维主管/审计=审计+配置、
// 只读查看者=看板只读）。角色值由 7.2 EIAM claims 同步映射；测试经 SetRole 注入。
type Role string

const (
	// RoleOpsEngineer 运维工程师：cert 台账/引用/变更读写。
	RoleOpsEngineer Role = "ops_engineer"
	// RoleOpsSupervisor 运维主管：审计+配置（settings/exemptions/crds/test）。
	RoleOpsSupervisor Role = "ops_supervisor"
	// RoleAuditor 审计：审计+配置（与运维主管同权限面）。
	RoleAuditor Role = "auditor"
	// RoleViewer 只读查看者：仅 dashboard+详情只读。
	RoleViewer Role = "viewer"
)

// CtxRoleKey gin 上下文中角色键：上游鉴权链（7.2 EIAM 接线，见
// role_middleware.go CertRoleMiddleware）写入，本包 RequireRoles 消费。
// 单角色=单值；多角色（如平台管理员双持能力码）=去重逗号串。
// 未设置视为未授权（deny-by-default）。
const CtxRoleKey = "cert_role"

// SetRole 写入单个角色到请求上下文（测试注入用；等价 SetRoles 单值）。
func SetRole(c *gin.Context, role Role) {
	SetRoles(c, role)
}

// SetRoles 写入角色集合（7.2 EIAM 映射：双持 cert:manage+cert:settings 或
// is_admin 的会话同时持有工程师+主管面）；去重保序。
func SetRoles(c *gin.Context, roles ...Role) {
	seen := make(map[Role]bool, len(roles))
	uniq := make([]string, 0, len(roles))
	for _, r := range roles {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		uniq = append(uniq, string(r))
	}
	c.Set(CtxRoleKey, strings.Join(uniq, ","))
}

// RoleFromContext 读取当前操作者首个角色；未设置返回空串。
func RoleFromContext(c *gin.Context) Role {
	roles := RolesFromContext(c)
	if len(roles) == 0 {
		return ""
	}
	return roles[0]
}

// RolesFromContext 读取当前操作者全部角色（逗号串拆分，空段过滤；
// 未知角色值保留但不会命中任何 RequireRoles 白名单）。
func RolesFromContext(c *gin.Context) []Role {
	raw := strings.TrimSpace(c.GetString(CtxRoleKey))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	roles := make([]Role, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			roles = append(roles, Role(p))
		}
	}
	return roles
}

// CodeForbidden EIAM 权限不足（api-handbook Error Codes：FORBIDDEN 403）。
const CodeForbidden = "FORBIDDEN"

// RequireRoles 角色门卫中间件：当前角色集合与允许集合无交集（含未设置）时
// 返回 403 FORBIDDEN 信封并 Abort；不做任何部分执行。
// Hard Rule（7.2）：权限拦截必须后端接口侧强制——本门卫即后端防线
// （ecmdb Casbin 全局策略检查之外的应用层三角色收敛）。
func RequireRoles(allowed ...Role) gin.HandlerFunc {
	allowedSet := make(map[Role]bool, len(allowed))
	for _, r := range allowed {
		allowedSet[r] = true
	}
	return func(c *gin.Context) {
		for _, r := range RolesFromContext(c) {
			if allowedSet[r] {
				c.Next()
				return
			}
		}
		WriteAPIError(c, http.StatusForbidden, CodeForbidden,
			"insufficient role for this operation")
		c.Abort()
	}
}

// operator 从上下文取操作者名（审计/豁免 operator 字段；认证中间件注入）。
func operator(c *gin.Context) string {
	return middleware.GetUsername(c)
}
