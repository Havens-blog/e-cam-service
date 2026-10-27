package middleware

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// S2SAuth 服务间调用鉴权中间件：校验 eiam 签发的 client_credentials RS256 JWT，
// 解出 tenant_id 注入租户上下文（TenantIDKey），供下游 RequireTenant 与 handler 复用。
//
// 与 e-cmdb-service 的 S2SAuth 不同：本服务日志查询按租户隔离（云账号属租户），
// 服务令牌必须携带租户上下文，否则 403。仅用于独立的 /api/s2s 服务间路由组，
// 不经用户会话中间件（EcmdbAuthMiddleware）。
//
// fail-closed：公钥未配置 / 无令牌 / 验签失败 / 令牌过期 / 缺租户 一律拒绝。
func S2SAuth(publicKeyPEM string) gin.HandlerFunc {
	pub := parseS2SPublicKey(publicKeyPEM)
	return func(c *gin.Context) {
		if pub == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "服务间鉴权公钥未配置或无效"})
			c.Abort()
			return
		}

		tokenStr := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "缺少服务令牌"})
			c.Abort()
			return
		}

		var claims struct {
			TenantID int64 `json:"tenant_id"`
			jwt.RegisteredClaims
		}
		token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodRS256 {
				return nil, fmt.Errorf("不支持的签名算法: %v", t.Header["alg"])
			}
			return pub, nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "服务令牌无效或已过期"})
			c.Abort()
			return
		}

		if claims.TenantID <= 0 {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "服务令牌缺少租户上下文"})
			c.Abort()
			return
		}

		// 注入租户：与 EcmdbAuthMiddleware 使用同一键，RequireTenant 与 handler 直接复用
		c.Set(TenantIDKey, claims.TenantID)
		c.Next()
	}
}

// parseS2SPublicKey 构造期解析一次公钥，失败返回 nil（中间件 fail-closed 拒绝）。
func parseS2SPublicKey(pem string) *rsa.PublicKey {
	if strings.TrimSpace(pem) == "" {
		return nil
	}
	pub, err := jwt.ParseRSAPublicKeyFromPEM([]byte(pem))
	if err != nil {
		return nil
	}
	return pub
}
