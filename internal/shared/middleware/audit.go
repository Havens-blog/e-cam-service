package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

const maxRequestBodySize = 4096

// 敏感字段列表
var sensitiveFields = map[string]bool{
	"password":          true,
	"secret_key":        true,
	"access_key":        true,
	"secret_id":         true,
	"access_key_secret": true,
}

// AuditMiddleware API 操作审计中间件
type AuditMiddleware struct {
	auditDAO dao.AuditLogDAO
	logger   *elog.Component
}

// NewAuditMiddleware 创建审计中间件
func NewAuditMiddleware(auditDAO dao.AuditLogDAO, logger *elog.Component) *AuditMiddleware {
	return &AuditMiddleware{auditDAO: auditDAO, logger: logger}
}

// auditResponseWriter 包装 ResponseWriter 以捕获状态码
type auditResponseWriter struct {
	gin.ResponseWriter
	statusCode int
}

func (w *auditResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Build 构建 gin 中间件
func (m *AuditMiddleware) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		// 仅拦截写操作
		if method == "GET" || method == "OPTIONS" || method == "HEAD" {
			c.Next()
			return
		}

		start := time.Now()

		// 读取并恢复 request body
		var bodyStr string
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				// 恢复 body 供后续 handler 使用
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				bodyStr = sanitizeBody(string(bodyBytes))
				if len(bodyStr) > maxRequestBodySize {
					bodyStr = bodyStr[:maxRequestBodySize]
				}
			}
		}

		// 包装 ResponseWriter
		writer := &auditResponseWriter{ResponseWriter: c.Writer, statusCode: 200}
		c.Writer = writer

		// 执行后续 handler
		c.Next()

		// 异步写入审计日志
		statusCode := writer.statusCode
		result := domain.AuditResultSuccess
		if statusCode >= 400 {
			result = domain.AuditResultFailed
		}

		uid := fmt.Sprintf("%d", GetUid(c))
		username := GetUsername(c)
		tenantID := GetTenantID(c)
		requestID := GetRequestID(c)
		path := c.Request.URL.Path
		opType := inferOperationType(path, method)
		durationMs := time.Since(start).Milliseconds()

		auditLog := domain.AuditLog{
			OperationType: opType,
			OperatorID:    uid,
			OperatorName:  username,
			TenantID:      tenantID,
			HTTPMethod:    method,
			APIPath:       path,
			RequestBody:   bodyStr,
			StatusCode:    statusCode,
			Result:        result,
			RequestID:     requestID,
			DurationMs:    durationMs,
			ClientIP:      c.ClientIP(),
			UserAgent:     c.Request.UserAgent(),
			Ctime:         time.Now().UnixMilli(),
		}

		// 异步写入，不阻塞请求
		go func() {
			ctx := context.WithoutCancel(context.Background())
			if _, err := m.auditDAO.Create(ctx, auditLog); err != nil {
				m.logger.Warn("写入审计日志失败",
					elog.FieldErr(err),
					elog.String("path", path),
					elog.String("method", method),
				)
			}
		}()
	}
}

// sanitizeBody 对请求体中的敏感字段脱敏
func sanitizeBody(body string) string {
	if body == "" {
		return body
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		// 非 JSON 格式，直接返回
		return body
	}
	sanitizeMap(data)
	result, err := json.Marshal(data)
	if err != nil {
		return body
	}
	return string(result)
}

func sanitizeMap(data map[string]interface{}) {
	for key, val := range data {
		if sensitiveFields[strings.ToLower(key)] {
			data[key] = "***"
			continue
		}
		// 递归处理嵌套对象
		if nested, ok := val.(map[string]interface{}); ok {
			sanitizeMap(nested)
		}
	}
}

// inferOperationType 根据 URL path 和 HTTP method 推断操作类型
func inferOperationType(path, method string) domain.AuditOperationType {
	// 提取资源名称
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/cam/"), "/")
	if len(parts) == 0 {
		return domain.AuditOpAPIGeneric
	}

	resource := parts[0]
	// 处理复数形式
	resource = strings.TrimSuffix(resource, "s")

	var action string
	switch method {
	case "POST":
		action = "create"
		// 特殊路径处理
		if len(parts) > 1 {
			lastPart := parts[len(parts)-1]
			if lastPart == "sync" {
				action = "sync"
			}
		}
	case "PUT", "PATCH":
		action = "update"
	case "DELETE":
		action = "delete"
	default:
		action = "generic"
	}

	return domain.AuditOperationType(fmt.Sprintf("api_%s_%s", resource, action))
}
