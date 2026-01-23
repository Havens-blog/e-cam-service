package web

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// RegisterSwaggerRoutes 注册 Swagger 文档路由
func RegisterSwaggerRoutes(router *gin.Engine) {
	// Swagger UI 页面
	router.GET("/docs", func(c *gin.Context) {
		c.File(filepath.Join("docs", "swagger-ui.html"))
	})

	// Swagger YAML 文件
	router.GET("/docs/swagger.yaml", func(c *gin.Context) {
		c.File(filepath.Join("docs", "swagger.yaml"))
	})

	// API 文档重定向
	router.GET("/api-docs", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/docs")
	})
}
