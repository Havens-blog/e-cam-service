package web

import (
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// Search 统一搜索资产
// @Summary 统一搜索资产
// @Description 跨资产类型搜索，支持按关键词匹配资产ID、名称、IP地址等。返回匹配信息供前端高亮显示
// @Tags 资产管理-搜索
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param keyword query string true "搜索关键词(匹配资产ID、名称、IP等)"
// @Param types query string false "资产类型(逗号分隔: ecs,rds,redis,mongodb,vpc,eip)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param account_id query int false "云账号ID"
// @Param region query string false "地域"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} SearchResultResponse "成功"
// @Failure 400 {object} ErrorResponse "参数错误"
// @Router /cam/assets/search [get]
func (h *AssetHandler) Search(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	keyword := ctx.Query("keyword")
	typesStr := ctx.Query("types")
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	accountIDStr := ctx.Query("account_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 解析资产类型
	var assetTypes []string
	if typesStr != "" {
		assetTypes = strings.Split(typesStr, ",")
	}

	filter := domain.SearchFilter{
		TenantID:   tenantID,
		Keyword:    keyword,
		AssetTypes: assetTypes,
		Provider:   provider,
		AccountID:  accountID,
		Region:     region,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	instances, total, err := h.instanceSvc.Search(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	// 转换为搜索结果，包含匹配信息
	items := h.toSearchResultVOs(instances, keyword)

	ctx.JSON(200, Result(SearchListResp{
		Items:   items,
		Total:   total,
		Keyword: keyword,
	}))
}

// ==================== 搜索结果结构体 ====================

// SearchListResp 搜索列表响应
type SearchListResp struct {
	Items   []SearchResultVO `json:"items"`
	Total   int64            `json:"total"`
	Keyword string           `json:"keyword"` // 返回搜索关键词，方便前端高亮
}

// SearchResultVO 搜索结果视图对象
type SearchResultVO struct {
	UnifiedAssetVO
	Matches []MatchInfo `json:"matches"` // 匹配信息，用于前端高亮
}

// MatchInfo 匹配信息
type MatchInfo struct {
	Field string `json:"field"` // 匹配的字段名
	Value string `json:"value"` // 匹配的字段值
	Label string `json:"label"` // 字段显示名称
}

// SearchResultResponse 搜索结果响应（用于 Swagger）
type SearchResultResponse struct {
	Code int            `json:"code" example:"0"`
	Msg  string         `json:"msg" example:"success"`
	Data SearchListResp `json:"data"`
}

// toSearchResultVOs 转换为搜索结果，包含匹配信息
func (h *AssetHandler) toSearchResultVOs(instances []domain.Instance, keyword string) []SearchResultVO {
	vos := make([]SearchResultVO, len(instances))
	for i, inst := range instances {
		vos[i] = h.toSearchResultVO(inst, keyword)
	}
	return vos
}

// toSearchResultVO 转换单个实例为搜索结果
func (h *AssetHandler) toSearchResultVO(inst domain.Instance, keyword string) SearchResultVO {
	baseVO := h.toUnifiedAssetVO(inst)
	matches := h.findMatches(inst, keyword)

	return SearchResultVO{
		UnifiedAssetVO: baseVO,
		Matches:        matches,
	}
}

// findMatches 查找匹配的字段
func (h *AssetHandler) findMatches(inst domain.Instance, keyword string) []MatchInfo {
	if keyword == "" {
		return nil
	}

	var matches []MatchInfo
	keywordLower := strings.ToLower(keyword)

	// 检查 asset_id
	if strings.Contains(strings.ToLower(inst.AssetID), keywordLower) {
		matches = append(matches, MatchInfo{
			Field: "asset_id",
			Value: inst.AssetID,
			Label: "资产ID",
		})
	}

	// 检查 asset_name
	if strings.Contains(strings.ToLower(inst.AssetName), keywordLower) {
		matches = append(matches, MatchInfo{
			Field: "asset_name",
			Value: inst.AssetName,
			Label: "资产名称",
		})
	}

	// 检查 attributes 中的常用字段
	if inst.Attributes != nil {
		// 内网IP
		if privateIP, ok := inst.Attributes["private_ip"].(string); ok {
			if strings.Contains(strings.ToLower(privateIP), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "private_ip",
					Value: privateIP,
					Label: "内网IP",
				})
			}
		}

		// 公网IP
		if publicIP, ok := inst.Attributes["public_ip"].(string); ok {
			if strings.Contains(strings.ToLower(publicIP), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "public_ip",
					Value: publicIP,
					Label: "公网IP",
				})
			}
		}

		// IP地址 (EIP)
		if ipAddress, ok := inst.Attributes["ip_address"].(string); ok {
			if strings.Contains(strings.ToLower(ipAddress), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "ip_address",
					Value: ipAddress,
					Label: "IP地址",
				})
			}
		}

		// 连接串 (数据库)
		if connStr, ok := inst.Attributes["connection_string"].(string); ok {
			if strings.Contains(strings.ToLower(connStr), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "connection_string",
					Value: connStr,
					Label: "连接地址",
				})
			}
		}

		// CIDR块 (VPC)
		if cidr, ok := inst.Attributes["cidr_block"].(string); ok {
			if strings.Contains(strings.ToLower(cidr), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "cidr_block",
					Value: cidr,
					Label: "CIDR块",
				})
			}
		}
	}

	return matches
}
