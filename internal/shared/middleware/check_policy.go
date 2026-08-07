package middleware

import (
	"context"
	"net/http"
	"time"

	policyv1 "github.com/Havens-blog/e-cam-service/api/proto/gen/ecmdb/policy/v1"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// PolicyConfig 策略检查中间件配置
type PolicyConfig struct {
	FailMode  string   `mapstructure:"fail_mode"` // fail_open / fail_closed
	Whitelist []string `mapstructure:"whitelist"` // 白名单路径
}

// CheckPolicyMiddleware 通过 gRPC 调用 ecmdb 的 Policy 服务做权限校验
type CheckPolicyMiddleware struct {
	policyClient policyv1.PolicyServiceClient
	logger       *elog.Component
	resource     string // 资源标识，用于区分 e-cam-service 的权限域
	failMode     string
	whitelist    []string
	cookieName   string // session cookie 名，用于 Authorization 头缺失时回退取 token
}

// NewCheckPolicyMiddleware 创建策略检查中间件
func NewCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient, cfg PolicyConfig, cookieName string, logger *elog.Component) *CheckPolicyMiddleware {
	failMode := cfg.FailMode
	if failMode == "" {
		failMode = "fail_open"
	}
	return &CheckPolicyMiddleware{
		policyClient: policyClient,
		logger:       logger,
		resource:     "CAM",
		failMode:     failMode,
		whitelist:    cfg.Whitelist,
		cookieName:   cookieName,
	}
}

// Build 构建 gin 中间件
func (m *CheckPolicyMiddleware) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果 policyClient 未初始化，直接放行
		if m.policyClient == nil {
			c.Next()
			return
		}

		// 白名单匹配
		if matchWhitelist(c.Request.URL.Path, m.whitelist) {
			c.Next()
			return
		}

		// 取 token：Authorization 头优先，回退 cookie（与认证层 carrier 一致）
		token := extractToken(c, m.cookieName)
		if token == "" {
			m.logger.Warn("策略检查: Token为空",
				elog.String("path", c.Request.URL.Path))
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "权限不足",
				"path":    c.Request.URL.Path,
			})
			c.Abort()
			return
		}

		path := c.Request.URL.Path
		method := c.Request.Method

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		resp, err := m.policyClient.Authorize(ctx, &policyv1.AuthorizeReq{
			Token:    token,
			Path:     path,
			Method:   method,
			Resource: m.resource,
		})

		if err != nil {
			if m.failMode == "fail_closed" {
				m.logger.Warn("策略检查 gRPC 调用失败，拒绝请求 (fail_closed)",
					elog.FieldErr(err),
					elog.String("path", path),
					elog.String("method", method),
				)
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"code":    503,
					"message": "权限服务暂时不可用，请稍后重试",
				})
				c.Abort()
				return
			}
			// fail_open: 放行并记录警告
			m.logger.Warn("策略检查 gRPC 调用失败，放行请求 (fail_open)",
				elog.FieldErr(err),
				elog.String("path", path),
				elog.String("method", method),
			)
			c.Next()
			return
		}

		if !resp.Allowed {
			m.logger.Debug("权限拒绝",
				elog.String("path", path),
				elog.String("method", method),
			)
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "权限不足",
				"path":    path,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractToken 取出请求携带的 token，顺序与认证层的 mixin TokenCarrier 保持一致：
// 先 Authorization 头，为空则回退 cookie。
//
// 认证中间件通过 mixin carrier 同时接受 header 与 cookie 两种载体，若此处只读
// header，纯 cookie 携带的请求会在通过认证后被本中间件判为「Token为空」而 403。
//
// cookie 分支补上 "Bearer " 前缀，使下游收到的格式与 header 分支一致（header
// 分支保持原样透传，不改变既有行为）。
func extractToken(c *gin.Context, cookieName string) string {
	if h := c.GetHeader("Authorization"); h != "" {
		return h
	}
	if cookieName == "" {
		return ""
	}
	if v, err := c.Cookie(cookieName); err == nil && v != "" {
		return "Bearer " + v
	}
	return ""
}
