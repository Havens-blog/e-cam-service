package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withClaims 注入会话与用户名（模拟 EcmdbAuthMiddleware 行为）。
func withClaims(data map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(session.CtxSessionKey, session.NewMemorySession(session.Claims{Data: data}))
		if u, ok := data["username"]; ok {
			c.Set(middleware.CtxUsernameKey, u)
		}
		c.Next()
	}
}

// ---- resolveCertRoles：EIAM claims → 三角色映射（7.2 AC-1）----

func TestResolveCertRoles_ExplicitClaim(t *testing.T) {
	// 显式角色声明优先（eiam 侧按角色码写入；支持逗号分隔多角色）
	assert.Equal(t, []Role{RoleAuditor}, resolveCertRoles(map[string]string{
		"cert_role": "auditor", "username": "u",
	}))
	assert.Equal(t, []Role{RoleOpsSupervisor, RoleOpsEngineer}, resolveCertRoles(map[string]string{
		"cert_role": "ops_supervisor,ops_engineer", "username": "u",
	}))
	// 未知角色值不映射（deny-by-default，不回退能力码推导）
	assert.Nil(t, resolveCertRoles(map[string]string{"cert_role": "superadmin", "username": "u"}))
}

func TestResolveCertRoles_Codes(t *testing.T) {
	// JSON 数组形态能力码（eiam GetAuthorizedCodes 序列化）
	assert.Equal(t, []Role{RoleOpsEngineer}, resolveCertRoles(map[string]string{
		"authorized_codes": `["cam:asset:view","cert:manage"]`, "username": "u",
	}))
	assert.Equal(t, []Role{RoleOpsSupervisor}, resolveCertRoles(map[string]string{
		"permissions": "cert:settings", "username": "u",
	}))
	// 双持（同时持有两码）→ 双角色面
	assert.Equal(t, []Role{RoleOpsSupervisor, RoleOpsEngineer}, resolveCertRoles(map[string]string{
		"authorized_codes": `["cert:manage","cert:settings"]`, "username": "u",
	}))
	// is_admin=true → 双角色（与前端 isAdmin=全量口径一致）
	assert.Equal(t, []Role{RoleOpsSupervisor, RoleOpsEngineer}, resolveCertRoles(map[string]string{
		"is_admin": "true", "username": "u",
	}))
}

func TestResolveCertRoles_DefaultViewer(t *testing.T) {
	// 已认证无 cert 能力码 → 只读查看者（PRD Story 6：查看者无需权限申请）
	assert.Equal(t, []Role{RoleViewer}, resolveCertRoles(map[string]string{
		"username": "biz@example.com", "tenant_id": "7",
	}))
	// 空 claims（无任何信号）→ nil：不写角色，deny-by-default
	assert.Nil(t, resolveCertRoles(map[string]string{}))
	assert.Nil(t, resolveCertRoles(nil))
}

// ---- CertRoleMiddleware：角色写入 + 操作者注入请求 ctx ----

func TestCertRoleMiddleware_RoleAndOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(withClaims(map[string]string{
		"username": "ops@example.com", "authorized_codes": `["cert:manage"]`,
	}))
	engine.Use(CertRoleMiddleware())
	var gotRole Role
	var gotOperator string
	engine.GET("/probe", func(c *gin.Context) {
		gotRole = RoleFromContext(c)
		gotOperator = service.OperatorFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, RoleOpsEngineer, gotRole)
	assert.Equal(t, "ops@example.com", gotOperator)
}

func TestCertRoleMiddleware_NoSessionNoRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CertRoleMiddleware())
	var roleSet bool
	engine.GET("/probe", func(c *gin.Context) {
		_, roleSet = c.Get(CtxRoleKey)
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.False(t, roleSet, "无会话时不得写角色（deny-by-default）")
}

// TestCertRoleMiddleware_DoesNotClobberInjectedRole 测试/上游已写角色时
// 中间件不覆盖（测试路由 withRole 注入路径与真实链共存）。
func TestCertRoleMiddleware_DoesNotClobberInjectedRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(withRole(RoleViewer)) // 先注入
	engine.Use(CertRoleMiddleware()) // 无会话 → 不动角色
	var got Role
	engine.GET("/probe", func(c *gin.Context) {
		got = RoleFromContext(c)
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, RoleViewer, got)
}
