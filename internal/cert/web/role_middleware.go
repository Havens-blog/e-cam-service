package web

import (
	"encoding/json"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------
// EIAM claims → cert 域角色映射 + 操作者注入（任务 7.2）。
//
// 三角色资源/动作映射（PRD Security Requirements；端点级白名单见各
// handler RegisterRoutes 的 RequireRoles 挂载）：
//   - 运维工程师（cert:manage）：cert 导入/删除/补传私钥、引用/扫描、
//     变更生成/确认/执行/续批/取消/回滚；
//   - 运维主管/审计（cert:settings）：settings/exemptions/crds/test 配置面
//     + 变更查看（列表/详情/审计流水）；
//   - 只读查看者（无能力码的已认证会话）：仅到期看板与证书详情只读。
//
// 能力码即 eiam Permission.Code（前端 src/utils/cert-permission.ts 同源
// 唯一对齐点）；端点物理资源经 middleware.RegisterEndpointsToEcmdb 启动期
// 批量同步 ecmdb endpoint 系统（沿用既有 EIAM 同步机制）。
// ---------------------------------------------------------------------

// EIAM 能力码常量（eiam Permission.Code；双持/is_admin → 双角色面）。
const (
	// EIAMCodeCertManage 证书管理（非只读）能力码：运维工程师面。
	EIAMCodeCertManage = "cert:manage"
	// EIAMCodeCertSettings 全局配置能力码：运维主管/审计面（与工程师面
	// 权限集不相交，双持者同时映射两角色）。
	EIAMCodeCertSettings = "cert:settings"
)

// certRoleClaimKey 显式角色声明键（claims.Data["cert_role"]，eiam 侧按
// 角色码写入时优先于能力码推导；支持逗号分隔多角色）。
const certRoleClaimKey = "cert_role"

// CertRoleMiddleware EIAM 会话 claims → cert 域角色（SetRoles）+ 操作者
// 注入请求 ctx（service.WithOperator，审计写入点归因消费）。
//
// 映射口径：cert_role 显式声明 > 能力码/is_admin 推导 > 只读查看者
// （PRD Story 6：查看者无需权限申请，已认证会话默认可见到期看板）。
// 会话缺失（未认证/白名单路径）不写角色——RequireRoles deny-by-default，
// 未认证请求在上游认证中间件已 401，不会到达本层。
func CertRoleMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if v, ok := c.Get(session.CtxSessionKey); ok {
			if sess, ok := v.(session.Session); ok {
				if roles := resolveCertRoles(sess.Claims().Data); len(roles) > 0 {
					SetRoles(c, roles...)
				}
			}
		}
		if u := middleware.GetUsername(c); u != "" {
			c.Request = c.Request.WithContext(service.WithOperator(c.Request.Context(), u))
		}
		c.Next()
	}
}

// resolveCertRoles claims → 角色集合（映射规则见 CertRoleMiddleware 注释；
// 全部信号缺失时返回 nil——调用方不写角色，deny-by-default。显式 cert_role
// 声明为权威信号：存在但无有效值时直接返回 nil（未知角色不静默降级为
// 查看者，最小权限口径），不再回退能力码推导）。
func resolveCertRoles(data map[string]string) []Role {
	if raw := strings.TrimSpace(data[certRoleClaimKey]); raw != "" {
		var roles []Role
		for _, p := range strings.Split(raw, ",") {
			if r := Role(strings.TrimSpace(p)); isValidRole(r) {
				roles = append(roles, r)
			}
		}
		return roles // 可为空（未知声明值）：显式信号的 deny 语义
	}
	codes := parseClaimCodes(data["authorized_codes"], data["permissions"])
	isAdmin := strings.EqualFold(strings.TrimSpace(data["is_admin"]), "true")
	var roles []Role
	if isAdmin || containsCode(codes, EIAMCodeCertSettings) {
		roles = append(roles, RoleOpsSupervisor) // 主管/审计同权限面，能力码不区分
	}
	if isAdmin || containsCode(codes, EIAMCodeCertManage) {
		roles = append(roles, RoleOpsEngineer)
	}
	if len(roles) == 0 && len(data) > 0 {
		roles = append(roles, RoleViewer) // 已认证无 cert 能力码 → 只读查看者
	}
	return roles
}

// isValidRole 已知角色校验（显式声明路径过滤未知值）。
func isValidRole(r Role) bool {
	switch r {
	case RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor, RoleViewer:
		return true
	}
	return false
}

// parseClaimCodes 解析能力码声明：JSON 数组字符串或逗号分隔（eiam claims
// 为 map[string]string，数组以序列化形态携带）；全部解析失败返回 nil。
func parseClaimCodes(raws ...string) []string {
	var codes []string
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "[") {
			var arr []string
			if err := json.Unmarshal([]byte(raw), &arr); err == nil {
				codes = append(codes, arr...)
				continue
			}
		}
		codes = append(codes, strings.Split(raw, ",")...)
	}
	return codes
}

// containsCode 能力码命中判定。
func containsCode(codes []string, code string) bool {
	for _, c := range codes {
		if strings.TrimSpace(c) == code {
			return true
		}
	}
	return false
}
