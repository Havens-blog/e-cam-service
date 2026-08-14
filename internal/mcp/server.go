package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server 多云资产管理 MCP Server
type Server struct {
	mcpServer *server.MCPServer
	deps      *Dependencies
}

// NewServer 创建 MCP Server 并注册所有 Tools
func NewServer(deps *Dependencies) *Server {
	s := &Server{deps: deps}

	s.mcpServer = server.NewMCPServer(
		"e-cam-service",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	s.registerTools()
	return s
}

// ServeStdio 以 stdio 模式启动 MCP Server
func (s *Server) ServeStdio(ctx context.Context) error {
	stdioServer := server.NewStdioServer(s.mcpServer)
	return stdioServer.Listen(ctx, nil, nil)
}

// registerTools 注册所有 MCP Tools
func (s *Server) registerTools() {
	s.registerAccountTools()
	s.registerAssetTools()
	s.registerSyncTools()
}

// ============================================================================
// 通用辅助函数
// ============================================================================

// getStringArg 从参数中安全获取字符串
func getStringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getIntArg 从参数中安全获取整数
func getIntArg(args map[string]interface{}, key string) int64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

// getStringSliceArg 从参数中安全获取字符串切片
func getStringSliceArg(args map[string]interface{}, key string) []string {
	if v, ok := args[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

// errorResult 返回错误结果
func errorResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("错误: %v", err))
}

// textResult 返回文本结果
func textResult(text string) *mcp.CallToolResult {
	return mcp.NewToolResultText(text)
}
