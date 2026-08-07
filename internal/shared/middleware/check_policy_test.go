package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// newTestCtx 构造一个带指定 header/cookie 的 gin.Context
func newTestCtx(headers map[string]string, cookies map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/whatever", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	c.Request = req
	return c
}

func TestExtractToken(t *testing.T) {
	const cookieName = "ecmdb-token-key"

	testCases := []struct {
		name    string
		headers map[string]string
		cookies map[string]string
		want    string
	}{
		{
			name:    "Authorization 头存在时直接使用",
			headers: map[string]string{"Authorization": "Bearer header.jwt.token"},
			want:    "Bearer header.jwt.token",
		},
		{
			name:    "无 Authorization 头时回退到 cookie，并补齐 Bearer 前缀",
			cookies: map[string]string{cookieName: "cookie.jwt.token"},
			want:    "Bearer cookie.jwt.token",
		},
		{
			name:    "两者都在时优先 header，与认证层 mixin carrier 顺序一致",
			headers: map[string]string{"Authorization": "Bearer header.jwt.token"},
			cookies: map[string]string{cookieName: "cookie.jwt.token"},
			want:    "Bearer header.jwt.token",
		},
		{
			name: "两者都不存在时返回空串",
			want: "",
		},
		{
			name:    "cookie 名不匹配时不误取",
			cookies: map[string]string{"some-other-cookie": "irrelevant"},
			want:    "",
		},
		{
			name:    "cookie 值为空串时视作不存在",
			cookies: map[string]string{cookieName: ""},
			want:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractToken(newTestCtx(tc.headers, tc.cookies), cookieName)
			assert.Equal(t, tc.want, got)
		})
	}
}

// cookieName 为空时不应回退到 cookie —— 避免配置缺失导致读错 cookie
func TestExtractToken_EmptyCookieName(t *testing.T) {
	c := newTestCtx(nil, map[string]string{"ecmdb-token-key": "cookie.jwt.token"})

	assert.Equal(t, "", extractToken(c, ""))
}

// policyClient 为 nil 时中间件直接放行，携带 cookie 的请求不应被 403
func TestCheckPolicyMiddleware_NilClientPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := NewCheckPolicyMiddleware(nil, PolicyConfig{}, "ecmdb-token-key", nil)

	engine := gin.New()
	engine.Use(m.Build())
	engine.GET("/api/v1/cam/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/ping", nil)
	req.AddCookie(&http.Cookie{Name: "ecmdb-token-key", Value: "cookie.jwt.token"})
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
