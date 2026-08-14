package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerSyncTools 注册同步与实时查询相关 Tools
func (s *Server) registerSyncTools() {
	// sync_assets - 触发资产同步
	s.mcpServer.AddTool(
		mcp.NewTool("sync_assets",
			mcp.WithDescription("触发指定云账号的资产同步任务。同步会从云厂商 API 拉取最新资产数据写入 CMDB。这是一个异步操作，返回任务ID。"),
			mcp.WithNumber("account_id",
				mcp.Required(),
				mcp.Description("云账号ID"),
			),
			mcp.WithString("asset_types",
				mcp.Description("要同步的资产类型，逗号分隔，如 'ecs,rds,redis'。为空则同步所有类型"),
			),
			mcp.WithString("regions",
				mcp.Description("要同步的地域，逗号分隔，如 'cn-hangzhou,cn-beijing'。为空则同步账号配置的所有地域"),
			),
		),
		s.handleSyncAssets,
	)

	// list_regions - 列出云账号可用地域
	s.mcpServer.AddTool(
		mcp.NewTool("list_regions",
			mcp.WithDescription("通过云厂商 API 实时获取指定云账号的可用地域列表。"),
			mcp.WithNumber("account_id",
				mcp.Required(),
				mcp.Description("云账号ID"),
			),
		),
		s.handleListRegions,
	)

	// realtime_list_ecs - 实时查询 ECS 实例
	s.mcpServer.AddTool(
		mcp.NewTool("realtime_list_ecs",
			mcp.WithDescription("通过云厂商 API 实时查询 ECS 云主机列表（不经过 CMDB）。适用于需要最新数据的场景，但速度较慢。"),
			mcp.WithNumber("account_id",
				mcp.Required(),
				mcp.Description("云账号ID"),
			),
			mcp.WithString("region",
				mcp.Required(),
				mcp.Description("地域，如 cn-hangzhou"),
			),
		),
		s.handleRealtimeListECS,
	)
}

func (s *Server) handleSyncAssets(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	accountID := getIntArg(args, "account_id")
	if accountID == 0 {
		return errorResult(fmt.Errorf("account_id 不能为空")), nil
	}

	// 解析资产类型
	var assetTypes []string
	if typesStr := getStringArg(args, "asset_types"); typesStr != "" {
		assetTypes = splitAndTrim(typesStr)
	}

	// 解析地域
	var regions []string
	if regionsStr := getStringArg(args, "regions"); regionsStr != "" {
		regions = splitAndTrim(regionsStr)
	}

	req := &shareddomain.SyncAccountRequest{
		AssetTypes: assetTypes,
		Regions:    regions,
	}

	result, err := s.deps.AccountSvc.SyncAccount(ctx, accountID, req)
	if err != nil {
		return errorResult(err), nil
	}

	detail := map[string]interface{}{
		"sync_id":    result.SyncID,
		"status":     result.Status,
		"message":    result.Message,
		"start_time": result.StartTime.Format("2006-01-02 15:04:05"),
	}

	data, _ := json.MarshalIndent(detail, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleListRegions(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountID := getIntArg(request.GetArguments(), "account_id")
	if accountID == 0 {
		return errorResult(fmt.Errorf("account_id 不能为空")), nil
	}

	// 获取账号（含凭证）
	account, err := s.deps.AccountSvc.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return errorResult(fmt.Errorf("获取云账号失败: %w", err)), nil
	}

	// 创建适配器
	adapter, err := s.deps.Factory.CreateAdapter(account)
	if err != nil {
		return errorResult(fmt.Errorf("创建云适配器失败: %w", err)), nil
	}

	// 获取地域列表
	regions, err := adapter.ECS().GetRegions(ctx)
	if err != nil {
		return errorResult(fmt.Errorf("获取地域列表失败: %w", err)), nil
	}

	type regionInfo struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		LocalName string `json:"local_name,omitempty"`
	}

	regionList := make([]regionInfo, 0, len(regions))
	for _, r := range regions {
		regionList = append(regionList, regionInfo{
			ID:        r.ID,
			Name:      r.Name,
			LocalName: r.LocalName,
		})
	}

	result := map[string]interface{}{
		"account_id": accountID,
		"provider":   string(account.Provider),
		"total":      len(regionList),
		"regions":    regionList,
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleRealtimeListECS(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	accountID := getIntArg(args, "account_id")
	if accountID == 0 {
		return errorResult(fmt.Errorf("account_id 不能为空")), nil
	}

	region := getStringArg(args, "region")
	if region == "" {
		return errorResult(fmt.Errorf("region 不能为空")), nil
	}

	// 获取账号（含凭证）
	account, err := s.deps.AccountSvc.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return errorResult(fmt.Errorf("获取云账号失败: %w", err)), nil
	}

	// 创建适配器
	adapter, err := s.deps.Factory.CreateAdapter(account)
	if err != nil {
		return errorResult(fmt.Errorf("创建云适配器失败: %w", err)), nil
	}

	// 实时查询 ECS
	instances, err := adapter.ECS().ListInstances(ctx, region)
	if err != nil {
		return errorResult(fmt.Errorf("查询 ECS 实例失败: %w", err)), nil
	}

	items := make([]map[string]interface{}, 0, len(instances))
	for _, inst := range instances {
		item := map[string]interface{}{
			"instance_id":   inst.InstanceID,
			"instance_name": inst.InstanceName,
			"status":        inst.Status,
			"instance_type": inst.InstanceType,
			"region":        inst.Region,
			"zone":          inst.Zone,
			"private_ip":    inst.PrivateIP,
			"os_type":       inst.OSType,
			"cpu":           inst.CPU,
			"memory_mb":     inst.Memory,
		}
		if inst.PublicIP != "" {
			item["public_ip"] = inst.PublicIP
		}
		if inst.VPCID != "" {
			item["vpc_id"] = inst.VPCID
		}
		items = append(items, item)
	}

	result := map[string]interface{}{
		"account_id": accountID,
		"provider":   string(account.Provider),
		"region":     region,
		"total":      len(items),
		"instances":  items,
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(data)), nil
}
