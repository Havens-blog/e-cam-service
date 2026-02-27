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

// CheckPolicyMiddleware 通过 gRPC 调用 ecmdb 的 Policy 服务做权限校验
type CheckPolicyMiddleware struct {
	policyClient policyv1.PolicyServiceClient
	logger       *elog.Component
	resource     string // 资源标识，用于区分 e-cam-service 的权限域
}

// NewCheckPolicyMiddleware 创建策略检查中间件
func NewCheckPolicyMiddleware(policyClient policyv1.PolicyServiceClient, logger *elog.Component) *CheckPolicyMiddleware {
	return &CheckPolicyMiddleware{
		policyClient: policyClient,
		logger:       logger,
		resource:     "CAM", // e-cam-service 的资源域标识
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

		uid := GetUid(c)
		if uid == 0 {
			m.logger.Warn("策略检查: 用户ID为空")
			c.AbortWithStatus(http.StatusForbidden)
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
			m.logger.Warn("策略检查 gRPC 调用失败，放行请求",
				elog.FieldErr(err),
				elog.String("path", path),
				elog.String("method", method),
				elog.Int64("uid", uid),
			)
			// gRPC 调用失败时放行，避免 ecmdb 不可用时阻塞所有请求
			// 生产环境如需严格控制，可改为 c.AbortWithStatus(http.StatusForbidden)
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
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
