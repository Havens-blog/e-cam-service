package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ecodeclub/ginx/gctx"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

const (
	// CtxUidKey 用户ID在上下文中的键
	CtxUidKey = "uid"
	// CtxUsernameKey 用户名在上下文中的键
	CtxUsernameKey = "username"
)

// AuthConfig 认证中间件配置
type AuthConfig struct {
	Whitelist []string `mapstructure:"whitelist"` // 白名单路径（精确匹配或前缀匹配 /path/*）
}

// EcmdbAuthMiddleware 复用 ecmdb 的 session 做认证（向后兼容）
// 用户在 ecmdb 登录后，e-cam-service 通过共享 Redis session 验证身份
func EcmdbAuthMiddleware(sp session.Provider, logger *elog.Component) gin.HandlerFunc {
	return EcmdbAuthMiddlewareWithConfig(sp, AuthConfig{}, logger)
}

// EcmdbAuthMiddlewareWithConfig 加固版认证中间件，支持白名单和结构化错误响应
func EcmdbAuthMiddlewareWithConfig(sp session.Provider, cfg AuthConfig, logger *elog.Component) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 白名单匹配
		if matchWhitelist(c.Request.URL.Path, cfg.Whitelist) {
			c.Next()
			return
		}

		gCtx := &gctx.Context{Context: c}
		sess, err := sp.Get(gCtx)
		if err != nil {
			logger.Debug("ecmdb session 认证失败",
				elog.FieldErr(err),
				elog.String("client_ip", c.ClientIP()),
				elog.String("path", c.Request.URL.Path),
			)
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "认证失败：会话无效或已过期",
			})
			c.Abort()
			return
		}

		claims := sess.Claims()

		// 将 ecmdb 的用户信息注入到 e-cam-service 的上下文
		c.Set(CtxUidKey, claims.Uid)
		if username, ok := claims.Data["username"]; ok {
			c.Set(CtxUsernameKey, username)
			// 设置调试响应头
			c.Header("X-Request-User", username)
		}

		// 同时注入 tenant_id（从 session 中获取，如果有的话）
		if tenantID, ok := claims.Data["tenant_id"]; ok && tenantID != "" {
			c.Set(TenantIDKey, tenantID)
		}

		// 保存 session 到上下文，供后续中间件使用
		c.Set(session.CtxSessionKey, sess)

		c.Next()
	}
}

// matchWhitelist 检查路径是否匹配白名单
func matchWhitelist(path string, whitelist []string) bool {
	for _, pattern := range whitelist {
		if strings.HasSuffix(pattern, "/*") {
			// 前缀匹配
			prefix := strings.TrimSuffix(pattern, "/*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		} else if path == pattern {
			// 精确匹配
			return true
		}
	}
	return false
}

// GetUid 从上下文中获取用户ID
func GetUid(c *gin.Context) int64 {
	if uid, exists := c.Get(CtxUidKey); exists {
		switch v := uid.(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case string:
			id, _ := strconv.ParseInt(v, 10, 64)
			return id
		}
	}
	return 0
}

// GetUsername 从上下文中获取用户名
func GetUsername(c *gin.Context) string {
	if username, exists := c.Get(CtxUsernameKey); exists {
		if u, ok := username.(string); ok {
			return u
		}
	}
	return ""
}
