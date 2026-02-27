package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// CtxRequestIDKey 请求ID在上下文中的键
	CtxRequestIDKey = "request_id"
	// RequestIDHeader 请求ID在请求头中的键
	RequestIDHeader = "X-Request-ID"
)

// RequestIDMiddleware 为每个请求生成或复用 X-Request-ID
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Set(CtxRequestIDKey, requestID)
		c.Header(RequestIDHeader, requestID)

		c.Next()
	}
}

// GetRequestID 从上下文中获取请求ID
func GetRequestID(c *gin.Context) string {
	if rid, exists := c.Get(CtxRequestIDKey); exists {
		if id, ok := rid.(string); ok {
			return id
		}
	}
	return ""
}
