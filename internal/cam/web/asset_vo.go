package web

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
)

// ==================== 响应结构体 ====================

// ImageStatsResp 镜像统计响应
type ImageStatsResp struct {
	Total  int64 `json:"total"`
	System int64 `json:"system"`
	Custom int64 `json:"custom"`
	Shared int64 `json:"shared"`
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
func extractAssetType(modelUID string) string {
	// cloud_vm -> ecs, cloud_rds -> rds, etc.
	switch modelUID {
	case "cloud_vm":
		return "ecs"
	case "cloud_disk":
		return "disk"
	case "cloud_snapshot":
		return "snapshot"
	case "cloud_security_group":
		return "security_group"
	case "cloud_rds":
		return "rds"
	case "cloud_redis":
		return "redis"
	case "cloud_mongodb":
		return "mongodb"
	case "cloud_vpc":
		return "vpc"
	case "cloud_eip":
		return "eip"
	case "cloud_lb", "cloud_slb", "cloud_alb", "cloud_nlb":
		return "lb"
	case "cloud_cdn":
		return "cdn"
	case "cloud_waf":
		return "waf"
	case "cloud_image":
		return "image"
	case "cloud_nas":
		return "nas"
	case "cloud_oss":
		return "oss"
	case "cloud_kafka":
		return "kafka"
	case "cloud_elasticsearch":
		return "elasticsearch"
	case "cloud_vswitch", "cloud_subnet":
		return "vswitch"
	}
	// aliyun_ecs -> ecs, aws_rds -> rds, etc.
	for _, suffix := range []string{"_ecs", "_disk", "_snapshot", "_security_group", "_rds", "_redis", "_mongodb", "_vpc", "_eip", "_vswitch", "_subnet", "_lb", "_slb", "_alb", "_nlb", "_cdn", "_waf", "_image", "_nas", "_oss", "_kafka", "_elasticsearch"} {
		if len(modelUID) > len(suffix) && modelUID[len(modelUID)-len(suffix):] == suffix {
			return suffix[1:] // 去掉前缀下划线
		}
	}
	return modelUID
}
