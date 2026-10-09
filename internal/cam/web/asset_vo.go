package web

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	shareddomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// ==================== 响应结构体 ====================

// ImageStatsResp 镜像统计响应
type ImageStatsResp struct {
	Total  int64 `json:"total"`
	System int64 `json:"system"`
	Custom int64 `json:"custom"`
	Shared int64 `json:"shared"`
	// Trend 周趋势(当前值 - 7 天前最近基线快照);无基线时为 nil
	Trend *ImageTrendResp `json:"trend,omitempty"`
}

// ImageTrendResp 镜像统计周趋势,各值为净变化(可为负)
type ImageTrendResp struct {
	BaselineDate string `json:"baseline_date"`
	Total        int64  `json:"total"`
	System       int64  `json:"system"`
	Custom       int64  `json:"custom"`
	Shared       int64  `json:"shared"`
}

// UnifiedAssetListResp 统一资产列表响应
type UnifiedAssetListResp struct {
	Items []UnifiedAssetVO `json:"items"`
	Total int64            `json:"total"`
}

// UnifiedAssetVO 统一资产视图对象
type UnifiedAssetVO struct {
	ID         int64                  `json:"id"`
	AssetID    string                 `json:"asset_id"`
	AssetName  string                 `json:"asset_name"`
	AssetType  string                 `json:"asset_type"`
	TenantID   int64                  `json:"tenant_id"`
	AccountID  int64                  `json:"account_id"`
	Provider   string                 `json:"provider"`
	Region     string                 `json:"region"`
	Status     string                 `json:"status"`
	Attributes map[string]interface{} `json:"attributes"`
	CreateTime int64                  `json:"create_time"`
	UpdateTime int64                  `json:"update_time"`
}

func (h *AssetHandler) toUnifiedAssetVOs(instances []domain.Instance) []UnifiedAssetVO {
	vos := make([]UnifiedAssetVO, len(instances))
	for i, inst := range instances {
		vos[i] = h.toUnifiedAssetVO(inst)
	}
	return vos
}

func (h *AssetHandler) toUnifiedAssetVO(inst domain.Instance) UnifiedAssetVO {
	provider := ""
	region := ""
	status := ""
	assetType := extractAssetType(inst.ModelUID)

	if inst.Attributes != nil {
		if p, ok := inst.Attributes["provider"].(string); ok {
			provider = p
		}
		if r, ok := inst.Attributes["region"].(string); ok {
			region = r
		}
		if s, ok := inst.Attributes["status"].(string); ok {
			status = s
		}
	}

	return UnifiedAssetVO{
		ID:         inst.ID,
		AssetID:    inst.AssetID,
		AssetName:  inst.AssetName,
		AssetType:  assetType,
		TenantID:   inst.TenantID,
		AccountID:  inst.AccountID,
		Provider:   provider,
		Region:     region,
		Status:     status,
		Attributes: inst.Attributes,
		CreateTime: inst.CreateTime.UnixMilli(),
		UpdateTime: inst.UpdateTime.UnixMilli(),
	}
}

// extractAssetType 从 model_uid 提取资产类型
// extractAssetType 从 model_uid 提取资产类型（注册表收敛：domain.ExtractAssetType）。
func extractAssetType(modelUID string) string {
	return shareddomain.ExtractAssetType(modelUID)
}
