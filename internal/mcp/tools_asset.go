package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerAssetTools 注册资产查询相关 Tools
func (s *Server) registerAssetTools() {
	// list_instances - 按类型列出资产实例
	s.mcpServer.AddTool(
		mcp.NewTool("list_instances",
			mcp.WithDescription("按资产类型列出云资产实例。支持 ECS、RDS、Redis、MongoDB、VPC、EIP 等所有资产类型。数据来自 CMDB（已同步的资产），响应速度快。"),
			mcp.WithNumber("tenant_id",
				mcp.Required(),
				mcp.Description("租户ID"),
			),
			mcp.WithString("asset_type",
				mcp.Required(),
				mcp.Description("资产类型: ecs, rds, redis, mongodb, vpc, eip, disk, snapshot, security_group, image, nas, oss, kafka, elasticsearch, vswitch, lb, cdn, waf"),
			),
			mcp.WithString("provider",
				mcp.Description("云厂商过滤: aliyun, aws, huawei, tencent, volcano"),
			),
			mcp.WithNumber("account_id",
				mcp.Description("云账号ID过滤"),
			),
			mcp.WithString("name",
				mcp.Description("按名称模糊搜索"),
			),
			mcp.WithNumber("offset",
				mcp.Description("分页偏移量，默认 0"),
			),
			mcp.WithNumber("limit",
				mcp.Description("每页数量，默认 20，最大 100"),
			),
		),
		s.handleListInstances,
	)

	// get_instance - 获取资产实例详情
	s.mcpServer.AddTool(
		mcp.NewTool("get_instance",
			mcp.WithDescription("获取单个资产实例的详细信息，包括所有属性（IP、配置、标签等）。"),
			mcp.WithNumber("instance_id",
				mcp.Required(),
				mcp.Description("实例的业务ID（数据库中的 ID）"),
			),
		),
		s.handleGetInstance,
	)

	// search_assets - 跨类型搜索资产
	s.mcpServer.AddTool(
		mcp.NewTool("search_assets",
			mcp.WithDescription("跨资产类型搜索。可按关键词匹配资产ID、名称、IP等字段。支持跨云、跨类型的统一搜索。"),
			mcp.WithNumber("tenant_id",
				mcp.Required(),
				mcp.Description("租户ID"),
			),
			mcp.WithString("keyword",
				mcp.Required(),
				mcp.Description("搜索关键词，匹配资产ID、名称、IP等"),
			),
			mcp.WithString("asset_types",
				mcp.Description("资产类型过滤，逗号分隔，如 'ecs,rds,redis'。为空则搜索所有类型"),
			),
			mcp.WithString("provider",
				mcp.Description("云厂商过滤: aliyun, aws, huawei, tencent, volcano"),
			),
			mcp.WithNumber("account_id",
				mcp.Description("云账号ID过滤"),
			),
			mcp.WithString("region",
				mcp.Description("地域过滤，如 cn-hangzhou"),
			),
			mcp.WithNumber("limit",
				mcp.Description("返回数量限制，默认 20"),
			),
		),
		s.handleSearchAssets,
	)

	// get_asset_statistics - 获取资产统计
	s.mcpServer.AddTool(
		mcp.NewTool("get_asset_statistics",
			mcp.WithDescription("获取资产统计概览：各类型资产数量、各云厂商资产分布。用于了解整体资产规模。"),
			mcp.WithNumber("tenant_id",
				mcp.Required(),
				mcp.Description("租户ID"),
			),
			mcp.WithNumber("account_id",
				mcp.Description("云账号ID过滤，不传则统计所有账号"),
			),
		),
		s.handleGetAssetStatistics,
	)
}

func (s *Server) handleListInstances(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	tenantID := getIntArg(args, "tenant_id")
	if tenantID == 0 {
		return errorResult(fmt.Errorf("tenant_id 不能为空")), nil
	}

	assetType := getStringArg(args, "asset_type")
	if assetType == "" {
		return errorResult(fmt.Errorf("asset_type 不能为空")), nil
	}

	// 构建 model_uid 匹配
	modelUID := mapAssetTypeToModelUID(assetType)

	limit := getIntArg(args, "limit")
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	filter := domain.InstanceFilter{
		ModelUID:  modelUID,
		TenantID:  tenantID,
		Provider:  getStringArg(args, "provider"),
		AccountID: getIntArg(args, "account_id"),
		AssetName: getStringArg(args, "name"),
		Offset:    getIntArg(args, "offset"),
		Limit:     limit,
	}

	instances, total, err := s.deps.listInstances(ctx, filter)
	if err != nil {
		return errorResult(err), nil
	}

	// 构建简洁输出
	items := make([]map[string]interface{}, 0, len(instances))
	for _, inst := range instances {
		item := map[string]interface{}{
			"id":         inst.ID,
			"asset_id":   inst.AssetID,
			"asset_name": inst.AssetName,
			"model_uid":  inst.ModelUID,
			"account_id": inst.AccountID,
		}

		// 提取常用属性
		if provider, ok := inst.Attributes["provider"]; ok {
			item["provider"] = provider
		}
		if region, ok := inst.Attributes["region"]; ok {
			item["region"] = region
		}
		if status, ok := inst.Attributes["status"]; ok {
			item["status"] = status
		}
		if privateIP, ok := inst.Attributes["private_ip"]; ok {
			item["private_ip"] = privateIP
		}
		if publicIP, ok := inst.Attributes["public_ip"]; ok && publicIP != "" {
			item["public_ip"] = publicIP
		}

		items = append(items, item)
	}

	result := map[string]interface{}{
		"total":      total,
		"asset_type": assetType,
		"items":      items,
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleGetInstance(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceID := getIntArg(request.GetArguments(), "instance_id")
	if instanceID == 0 {
		return errorResult(fmt.Errorf("instance_id 不能为空")), nil
	}

	instance, err := s.deps.InstanceSvc.GetByID(ctx, instanceID)
	if err != nil {
		return errorResult(err), nil
	}

	detail := map[string]interface{}{
		"id":          instance.ID,
		"asset_id":    instance.AssetID,
		"asset_name":  instance.AssetName,
		"model_uid":   instance.ModelUID,
		"tenant_id":   instance.TenantID,
		"account_id":  instance.AccountID,
		"attributes":  instance.Attributes,
		"create_time": instance.CreateTime.Format("2006-01-02 15:04:05"),
		"update_time": instance.UpdateTime.Format("2006-01-02 15:04:05"),
	}

	data, _ := json.MarshalIndent(detail, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleSearchAssets(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	tenantID := getIntArg(args, "tenant_id")
	if tenantID == 0 {
		return errorResult(fmt.Errorf("tenant_id 不能为空")), nil
	}

	keyword := getStringArg(args, "keyword")
	if keyword == "" {
		return errorResult(fmt.Errorf("keyword 不能为空")), nil
	}

	limit := getIntArg(args, "limit")
	if limit <= 0 {
		limit = 20
	}

	// 解析 asset_types
	var assetTypes []string
	if typesStr := getStringArg(args, "asset_types"); typesStr != "" {
		for _, t := range splitAndTrim(typesStr) {
			if t != "" {
				assetTypes = append(assetTypes, t)
			}
		}
	}

	filter := domain.SearchFilter{
		TenantID:   tenantID,
		Keyword:    keyword,
		AssetTypes: assetTypes,
		Provider:   getStringArg(args, "provider"),
		AccountID:  getIntArg(args, "account_id"),
		Region:     getStringArg(args, "region"),
		Limit:      limit,
	}

	instances, total, err := s.deps.searchInstances(ctx, filter)
	if err != nil {
		return errorResult(err), nil
	}

	items := make([]map[string]interface{}, 0, len(instances))
	for _, inst := range instances {
		item := map[string]interface{}{
			"id":         inst.ID,
			"asset_id":   inst.AssetID,
			"asset_name": inst.AssetName,
			"model_uid":  inst.ModelUID,
			"account_id": inst.AccountID,
		}
		if provider, ok := inst.Attributes["provider"]; ok {
			item["provider"] = provider
		}
		if region, ok := inst.Attributes["region"]; ok {
			item["region"] = region
		}
		if status, ok := inst.Attributes["status"]; ok {
			item["status"] = status
		}
		items = append(items, item)
	}

	result := map[string]interface{}{
		"total":   total,
		"keyword": keyword,
		"items":   items,
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(data)), nil
}

func (s *Server) handleGetAssetStatistics(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	tenantID := getIntArg(args, "tenant_id")
	if tenantID == 0 {
		return errorResult(fmt.Errorf("tenant_id 不能为空")), nil
	}

	accountID := getIntArg(args, "account_id")

	// 统计各类型资产数量
	assetTypes := []string{
		"ecs", "rds", "redis", "mongodb",
		"vpc", "eip", "vswitch", "lb",
		"disk", "snapshot", "security_group", "image",
		"nas", "oss", "kafka", "elasticsearch",
		"cdn", "waf",
	}

	stats := make(map[string]int64)
	var totalCount int64

	for _, assetType := range assetTypes {
		filter := domain.InstanceFilter{
			ModelUID:  mapAssetTypeToModelUID(assetType),
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     0, // 只需要 count
		}
		_, count, err := s.deps.listInstances(ctx, filter)
		if err != nil {
			continue
		}
		if count > 0 {
			stats[assetType] = count
			totalCount += count
		}
	}

	result := map[string]interface{}{
		"tenant_id":   tenantID,
		"total_count": totalCount,
		"by_type":     stats,
	}

	if accountID > 0 {
		result["account_id"] = accountID
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(data)), nil
}

// ============================================================================
// 辅助函数
// ============================================================================

// mapAssetTypeToModelUID 将资产类型映射为 CMDB model_uid
func mapAssetTypeToModelUID(assetType string) string {
	mapping := map[string]string{
		"ecs":            "ecs",
		"rds":            "rds",
		"redis":          "redis",
		"mongodb":        "mongodb",
		"vpc":            "vpc",
		"eip":            "eip",
		"vswitch":        "vswitch",
		"lb":             "lb",
		"disk":           "disk",
		"snapshot":       "snapshot",
		"security_group": "security_group",
		"image":          "image",
		"nas":            "nas",
		"oss":            "oss",
		"kafka":          "kafka",
		"elasticsearch":  "elasticsearch",
		"cdn":            "cdn",
		"waf":            "waf",
	}

	if uid, ok := mapping[assetType]; ok {
		return uid
	}
	return assetType
}

// splitAndTrim 按逗号分割并去除空格
func splitAndTrim(s string) []string {
	var result []string
	for _, part := range splitString(s, ',') {
		trimmed := trimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitString(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}
