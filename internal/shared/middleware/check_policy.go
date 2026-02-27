package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	policyv1 "github.com/Duke1616/ecmdb/api/proto/gen/ecmdb/policy/v1"
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
}

// NewCheckPolicyMiddleware 创建策略检查中间件
func NewCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient, cfg PolicyConfig, logger *elog.Component) *CheckPolicyMiddleware {
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

		uid := GetUid(c)
		if uid == 0 {
			m.logger.Warn("策略检查: 用户ID为空")
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
			UserId:   strconv.FormatInt(uid, 10),
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
					elog.Int64("uid", uid),
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
				elog.Int64("uid", uid),
			)
			c.Next()
			return
		}

		if !resp.Allowed {
			m.logger.Debug("权限拒绝",
				elog.String("path", path),
				elog.String("method", method),
				elog.Int64("uid", uid),
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
