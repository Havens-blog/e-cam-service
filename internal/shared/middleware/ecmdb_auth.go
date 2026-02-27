package middleware

import (
	"net/http"
	"strconv"

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

// EcmdbAuthMiddleware 复用 ecmdb 的 session 做认证
// 用户在 ecmdb 登录后，e-cam-service 通过共享 Redis session 验证身份
func EcmdbAuthMiddleware(sp session.Provider, logger *elog.Component) gin.HandlerFunc {
	return func(c *gin.Context) {
		gCtx := &gctx.Context{Context: c}
		sess, err := sp.Get(gCtx)
		if err != nil {
			logger.Debug("ecmdb session 认证失败", elog.FieldErr(err))
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		claims := sess.Claims()

		// 将 ecmdb 的用户信息注入到 e-cam-service 的上下文
		c.Set(CtxUidKey, claims.Uid)
		if username, ok := claims.Data["username"]; ok {
			c.Set(CtxUsernameKey, username)
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
