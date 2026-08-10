package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name string
		set  func(c *gin.Context)
		want int64
	}{
		{
			name: "认证中间件已写入 int64 租户",
			set:  func(c *gin.Context) { c.Set(TenantIDKey, int64(7)) },
			want: 7,
		},
		{
			name: "未设置时返回 0（视作未选定租户）",
			set:  func(c *gin.Context) {},
			want: 0,
		},
		{
			name: "类型不符时返回 0 而非 panic",
			set:  func(c *gin.Context) { c.Set(TenantIDKey, "Jlc") },
			want: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			tc.set(c)
			assert.Equal(t, tc.want, GetTenantID(c))
		})
	}
}

// 客户端自报的租户必须被忽略：header 与查询参数都不再是租户来源
func TestGetTenantID_IgnoresClientSuppliedTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/x?tenant_id=999", nil)
	req.Header.Set("X-Tenant-ID", "999")
	c.Request = req

	assert.Equal(t, int64(0), GetTenantID(c))
}

func TestRequireTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		tenantID   any
		wantStatus int
	}{
		{name: "有效租户放行", tenantID: int64(7), wantStatus: http.StatusOK},
		{name: "租户为 0 拒绝", tenantID: int64(0), wantStatus: http.StatusForbidden},
		{name: "租户缺失拒绝", tenantID: nil, wantStatus: http.StatusForbidden},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				if tc.tenantID != nil {
					c.Set(TenantIDKey, tc.tenantID)
				}
				c.Next()
			})
			engine.Use(RequireTenant(nil))
			engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
			assert.Equal(t, tc.wantStatus, w.Code)
		})
	}
}
