package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// unmatchedRoute 未匹配路由（404 等）的 route 标签兜底值：gin 的
// c.FullPath() 对无路由命中的请求返回空串，若直接使用会产生空标签维度，
// 若回退原始路径则会把任意 404 探测路径灌进指标基数，故归并为固定值。
const unmatchedRoute = "unmatched"

// routeLabel 返回低基数路由标签：命中路由用路由模板（如
// /api/v1/cam/assets/:id），未命中归并为 "unmatched"。
func routeLabel(c *gin.Context) string {
	if fp := c.FullPath(); fp != "" {
		return fp
	}
	return unmatchedRoute
}

// PrometheusMiddleware 记录 HTTP 请求计数与耗时（method/route/status）。
//
// 必须注册在认证中间件之前以覆盖全部请求；按任务口径，免认证路由
// （/metrics、/swagger、健康检查）同样计入，route 标签用路由模板保持低基数。
//
// 已知边界：gin.Default() 内置的 Recovery 挂在本中间件外侧，handler panic
// 时本中间件 c.Next() 之后的计数字段不会执行，该类请求不计入指标
// （Recovery 自身有日志兜底）。
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := routeLabel(c)
		status := strconv.Itoa(c.Writer.Status())
		HTTPRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		HTTPRequestDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}
