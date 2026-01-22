package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

const (
	// TenantIDKey 租户ID在上下文中的键
	TenantIDKey = "tenant_id"
	// TenantIDHeader 租户ID在请求头中的键
	TenantIDHeader = "X-Tenant-ID"
)

// TenantMiddleware 租户中间件
// 从请求头或 JWT Token 中提取租户ID，并注入到上下文中
func TenantMiddleware(logger *elog.Component) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 优先从请求头获取租户ID
		tenantID := c.GetHeader(TenantIDHeader)

		// 2. 如果请求头没有，尝试从 JWT Token 中获取（如果你使用了 JWT）
		if tenantID == "" {
			// 从 JWT claims 中获取
			if claims, exists := c.Get("claims"); exists {
				if claimsMap, ok := claims.(map[string]interface{}); ok {
					if tid, ok := claimsMap["tenant_id"].(string); ok {
						tenantID = tid
					}
				}
			}
		}

		// 3. 如果还是没有，可以从查询参数获取（不推荐，仅用于开发测试）
		if tenantID == "" {
			tenantID = c.Query("tenant_id")
		}

		// 4. 将租户ID注入到上下文
		if tenantID != "" {
			c.Set(TenantIDKey, tenantID)
			logger.Debug("tenant context set", elog.String("tenant_id", tenantID))
		} else {
			logger.Warn("no tenant id found in request")
		}

		c.Next()
	}
}

// GetTenantID 从上下文中获取租户ID
func GetTenantID(c *gin.Context) string {
	if tenantID, exists := c.Get(TenantIDKey); exists {
		if tid, ok := tenantID.(string); ok {
			return tid
		}
	}
	return ""
}

// RequireTenant 要求必须有租户ID的中间件
func RequireTenant(logger *elog.Component) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := GetTenantID(c)
		if tenantID == "" {
			logger.Warn("tenant id required but not found")
			c.JSON(400, gin.H{
				"code":    400,
				"message": "租户ID不能为空",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
