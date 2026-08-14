package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerAccountTools 注册云账号相关 Tools
func (s *Server) registerAccountTools() {
	// list_accounts - 列出云账号
	s.mcpServer.AddTool(
		mcp.NewTool("list_accounts",
			mcp.WithDescription("列出云账号列表。可按云厂商、环境、状态过滤。返回账号名称、云厂商、状态、地域等信息（凭证已脱敏）。"),
			mcp.WithNumber("tenant_id",
				mcp.Required(),
				mcp.Description("租户ID"),
			),
			mcp.WithString("provider",
				mcp.Description("云厂商过滤: aliyun, aws, huawei, tencent, volcano"),
				mcp.Enum("aliyun", "aws", "huawei", "tencent", "volcano"),
			),
			mcp.WithString("status",
				mcp.Description("账号状态过滤: active, disabled, error"),
				mcp.Enum("active", "disabled", "error"),
			),
		),
		s.handleListAccounts,
	)

	// get_account - 获取云账号详情
	s.mcpServer.AddTool(
		mcp.NewTool("get_account",
			mcp.WithDescription("获取指定云账号的详细信息，包括名称、云厂商、地域、状态、最后同步时间等（凭证已脱敏）。"),
			mcp.WithNumber("account_id",
				mcp.Required(),
				mcp.Description("云账号ID"),
			),
		),
		s.handleGetAccount,
	)

	// test_account_connection - 测试云账号连接
	s.mcpServer.AddTool(
		mcp.NewTool("test_account_connection",
			mcp.WithDescription("测试云账号的连接是否正常，验证 AK/SK 凭证有效性。"),
			mcp.WithNumber("account_id",
				mcp.Required(),
				mcp.Description("云账号ID"),
			),
		),
		s.handleTestConnection,
	)
}

func (s *Server) handleListAccounts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tenantID := getIntArg(request.GetArguments(), "tenant_id")
	if tenantID == 0 {
		return errorResult(fmt.Errorf("tenant_id 不能为空")), nil
	}

	filter := domain.CloudAccountFilter{
		TenantID: tenantID,
		Limit:    50,
	}

	if provider := getStringArg(request.GetArguments(), "provider"); provider != "" {
		filter.Provider = domain.CloudProvider(provider)
	}
	if status := getStringArg(request.GetArguments(), "status"); status != "" {
		filter.Status = domain.CloudAccountStatus(status)
	}

	accounts, total, err := s.deps.AccountSvc.ListAccounts(ctx, filter)
	if err != nil {
		return errorResult(err), nil
	}

	// 构建简洁的输出
	type accountSummary struct {
		ID           int64    `json:"id"`
		Name         string   `json:"name"`
		Provider     string   `json:"provider"`
		Environment  string   `json:"environment"`
		Status       string   `json:"status"`
		Regions      []string `json:"regions"`
		AssetCount   int64    `json:"asset_count"`
		LastSyncTime string   `json:"last_sync_time,omitempty"`
		Description  string   `json:"description,omitempty"`
	}

	summaries := make([]accountSummary, 0, len(accounts))
	for _, acc := range accounts {
		summary := accountSummary{
			ID:          acc.ID,
			Name:        acc.Name,
			Provider:    string(acc.Provider),
			Environment: string(acc.Environment),
			Status:      string(acc.Status),
			Regions:     acc.Regions,
			AssetCount:  acc.AssetCount,
			Description: acc.Description,
		}
		if acc.LastSyncTime != nil {
			summary.LastSyncTime = acc.LastSyncTime.Format("2006-01-02 15:04:05")
		}
		summaries = append(summaries, summary)
	}

	result := map[string]interface{}{
		"total":    total,
		"accounts": summaries,
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleGetAccount(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountID := getIntArg(request.GetArguments(), "account_id")
	if accountID == 0 {
		return errorResult(fmt.Errorf("account_id 不能为空")), nil
	}

	account, err := s.deps.AccountSvc.GetAccount(ctx, accountID)
	if err != nil {
		return errorResult(err), nil
	}

	// 构建详情输出（凭证已在 service 层脱敏）
	detail := map[string]interface{}{
		"id":          account.ID,
		"name":        account.Name,
		"provider":    string(account.Provider),
		"environment": string(account.Environment),
		"status":      string(account.Status),
		"regions":     account.Regions,
		"asset_count": account.AssetCount,
		"description": account.Description,
		"config": map[string]interface{}{
			"enable_auto_sync":       account.Config.EnableAutoSync,
			"sync_interval_minutes":  account.Config.SyncInterval,
			"read_only":              account.Config.ReadOnly,
			"enable_cost_monitoring": account.Config.EnableCostMonitoring,
		},
		"create_time": account.CreateTime.Format("2006-01-02 15:04:05"),
		"update_time": account.UpdateTime.Format("2006-01-02 15:04:05"),
	}

	if account.LastSyncTime != nil {
		detail["last_sync_time"] = account.LastSyncTime.Format("2006-01-02 15:04:05")
	}
	if account.ErrorMessage != "" {
		detail["error_message"] = account.ErrorMessage
	}

	data, _ := json.MarshalIndent(detail, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleTestConnection(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountID := getIntArg(request.GetArguments(), "account_id")
	if accountID == 0 {
		return errorResult(fmt.Errorf("account_id 不能为空")), nil
	}

	result, err := s.deps.AccountSvc.TestConnection(ctx, accountID)
	if err != nil {
		return errorResult(err), nil
	}

	detail := map[string]interface{}{
		"status":    result.Status,
		"message":   result.Message,
		"test_time": result.TestTime.Format("2006-01-02 15:04:05"),
	}
	if len(result.Regions) > 0 {
		detail["available_regions"] = result.Regions
	}

	data, _ := json.MarshalIndent(detail, "", "  ")
	return textResult(string(data)), nil
}
