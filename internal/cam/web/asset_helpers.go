package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ==================== 通用方法 ====================

// listAssets 通用的资产列表查询
func (h *AssetHandler) listAssets(ctx *gin.Context, assetType string) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 构建属性过滤条件
	attributes := make(map[string]interface{})
	if region != "" {
		attributes["region"] = region
	}
	if status != "" {
		attributes["status"] = status
	}

	filter := domain.InstanceFilter{
		ModelUID:   assetType, // DAO层会自动转换为正则匹配
		TenantID:   tenantID,
		AccountID:  accountID,
		AssetName:  name,
		Provider:   provider,
		Attributes: attributes,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	// 支持按 vpc_id 过滤（用于子网/安全组查询）
	vpcID := ctx.Query("vpc_id")
	if vpcID != "" {
		filter.Attributes["vpc_id"] = vpcID
	}

	instances, total, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(UnifiedAssetListResp{
		Items: h.toUnifiedAssetVOs(instances),
		Total: total,
	}))
}

// getAsset 通用的资产详情查询
func (h *AssetHandler) getAsset(ctx *gin.Context, assetType string) {
	assetID := ctx.Param("asset_id")
	if assetID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.ParamsError, "asset_id is required"))
		return
	}

	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")

	// 通过 asset_id 搜索
	filter := domain.InstanceFilter{
		AssetID:  assetID,
		TenantID: tenantID,
		Provider: provider,
		Limit:    10,
	}

	instances, _, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	// 过滤出匹配资产类型的实例
	for _, inst := range instances {
		if matchAssetType(inst.ModelUID, assetType) {
			ctx.JSON(200, Result(h.toUnifiedAssetVO(inst)))
			return
		}
	}

	ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
}

// matchAssetType 检查 model_uid 是否匹配资产类型
func matchAssetType(modelUID, assetType string) bool {
	switch assetType {
	case "ecs":
		return modelUID == "ecs" || modelUID == "cloud_vm" ||
			modelUID == "aliyun_ecs" ||
			modelUID == "aws_ecs" ||
			modelUID == "huawei_ecs" ||
			modelUID == "tencent_ecs" ||
			modelUID == "volcano_ecs" || modelUID == "volcengine_ecs"
	case "disk":
		return modelUID == "disk" || modelUID == "cloud_disk" ||
			modelUID == "aliyun_disk" ||
			modelUID == "aws_disk" ||
			modelUID == "huawei_disk" ||
			modelUID == "tencent_disk" ||
			modelUID == "volcano_disk" || modelUID == "volcengine_disk"
	case "snapshot":
		return modelUID == "snapshot" || modelUID == "cloud_snapshot" ||
			modelUID == "aliyun_snapshot" ||
			modelUID == "aws_snapshot" ||
			modelUID == "huawei_snapshot" ||
			modelUID == "tencent_snapshot" ||
			modelUID == "volcano_snapshot" || modelUID == "volcengine_snapshot"
	case "security_group":
		return modelUID == "security_group" || modelUID == "cloud_security_group" ||
			modelUID == "aliyun_security_group" ||
			modelUID == "aws_security_group" ||
			modelUID == "huawei_security_group" ||
			modelUID == "tencent_security_group" ||
			modelUID == "volcano_security_group" || modelUID == "volcengine_security_group"
	case "rds":
		return modelUID == "rds" || modelUID == "cloud_rds" ||
			modelUID == "aliyun_rds" ||
			modelUID == "aws_rds" ||
			modelUID == "huawei_rds" ||
			modelUID == "tencent_rds" ||
			modelUID == "volcano_rds" || modelUID == "volcengine_rds"
	case "redis":
		return modelUID == "redis" || modelUID == "cloud_redis" ||
			modelUID == "aliyun_redis" ||
			modelUID == "aws_redis" ||
			modelUID == "huawei_redis" ||
			modelUID == "tencent_redis" ||
			modelUID == "volcano_redis" || modelUID == "volcengine_redis"
	case "mongodb":
		return modelUID == "mongodb" || modelUID == "cloud_mongodb" ||
			modelUID == "aliyun_mongodb" ||
			modelUID == "aws_mongodb" ||
			modelUID == "huawei_mongodb" ||
			modelUID == "tencent_mongodb" ||
			modelUID == "volcano_mongodb" || modelUID == "volcengine_mongodb"
	case "vpc":
		return modelUID == "vpc" || modelUID == "cloud_vpc" ||
			modelUID == "aliyun_vpc" ||
			modelUID == "aws_vpc" ||
			modelUID == "huawei_vpc" ||
			modelUID == "tencent_vpc" ||
			modelUID == "volcano_vpc" || modelUID == "volcengine_vpc"
	case "subnet", "vswitch":
		return modelUID == "subnet" || modelUID == "vswitch" ||
			modelUID == "cloud_subnet" || modelUID == "cloud_vswitch" ||
			modelUID == "aliyun_vswitch" ||
			modelUID == "aws_subnet" || modelUID == "aws_vswitch" ||
			modelUID == "huawei_subnet" || modelUID == "huawei_vswitch" ||
			modelUID == "tencent_subnet" || modelUID == "tencent_vswitch" ||
			modelUID == "volcano_subnet" || modelUID == "volcano_vswitch" ||
			modelUID == "volcengine_subnet" || modelUID == "volcengine_vswitch"
	case "eip":
		return modelUID == "eip" || modelUID == "cloud_eip" ||
			modelUID == "aliyun_eip" ||
			modelUID == "aws_eip" ||
			modelUID == "huawei_eip" ||
			modelUID == "tencent_eip" ||
			modelUID == "volcano_eip" || modelUID == "volcengine_eip"
	case "lb":
		return modelUID == "lb" || modelUID == "cloud_lb" ||
			modelUID == "slb" || modelUID == "alb" || modelUID == "nlb" ||
			modelUID == "aliyun_lb" || modelUID == "aliyun_slb" || modelUID == "aliyun_alb" || modelUID == "aliyun_nlb" ||
			modelUID == "aws_lb" || modelUID == "aws_elb" || modelUID == "aws_alb" || modelUID == "aws_nlb" ||
			modelUID == "huawei_lb" || modelUID == "huawei_elb" ||
			modelUID == "tencent_lb" || modelUID == "tencent_clb" ||
			modelUID == "volcano_lb" || modelUID == "volcengine_lb"
	case "nas":
		return modelUID == "nas" || modelUID == "cloud_nas" ||
			modelUID == "aliyun_nas" ||
			modelUID == "aws_nas" ||
			modelUID == "huawei_nas" ||
			modelUID == "tencent_nas" ||
			modelUID == "volcano_nas" || modelUID == "volcengine_nas"
	case "oss":
		return modelUID == "oss" || modelUID == "cloud_oss" ||
			modelUID == "aliyun_oss" ||
			modelUID == "aws_oss" ||
			modelUID == "huawei_oss" ||
			modelUID == "tencent_oss" ||
			modelUID == "volcano_oss" || modelUID == "volcengine_oss"
	case "kafka":
		return modelUID == "kafka" || modelUID == "cloud_kafka" ||
			modelUID == "aliyun_kafka" ||
			modelUID == "aws_kafka" ||
			modelUID == "huawei_kafka" ||
			modelUID == "tencent_kafka" ||
			modelUID == "volcano_kafka" || modelUID == "volcengine_kafka"
	case "elasticsearch":
		return modelUID == "elasticsearch" || modelUID == "cloud_elasticsearch" ||
			modelUID == "aliyun_elasticsearch" ||
			modelUID == "aws_elasticsearch" ||
			modelUID == "huawei_elasticsearch" ||
			modelUID == "tencent_elasticsearch" ||
			modelUID == "volcano_elasticsearch" || modelUID == "volcengine_elasticsearch"
	case "cdn":
		return modelUID == "cdn" || modelUID == "cloud_cdn" ||
			modelUID == "aliyun_cdn" ||
			modelUID == "aws_cdn" ||
			modelUID == "huawei_cdn" ||
			modelUID == "tencent_cdn" ||
			modelUID == "volcano_cdn" || modelUID == "volcengine_cdn"
	case "waf":
		return modelUID == "waf" || modelUID == "cloud_waf" ||
			modelUID == "aliyun_waf" ||
			modelUID == "aws_waf" ||
			modelUID == "huawei_waf" ||
			modelUID == "tencent_waf" ||
			modelUID == "volcano_waf" || modelUID == "volcengine_waf"
	case "eni":
		return modelUID == "eni" || modelUID == "cloud_eni" ||
			modelUID == "aliyun_eni" ||
			modelUID == "aws_eni" ||
			modelUID == "huawei_eni" ||
			modelUID == "tencent_eni" ||
			modelUID == "volcano_eni" || modelUID == "volcengine_eni"
	case "image":
		return modelUID == "image" || modelUID == "cloud_image" ||
			modelUID == "aliyun_image" ||
			modelUID == "aws_image" ||
			modelUID == "huawei_image" ||
			modelUID == "tencent_image" ||
			modelUID == "volcano_image" || modelUID == "volcengine_image"
	}
	return false
}

// ==================== 辅助函数 ====================

// extractStringArray 从 interface{} 提取字符串数组
// 支持 []interface{}, []string, primitive.A 等类型
func extractStringArray(v interface{}) []string {
	if v == nil {
		return nil
	}

	var result []string

	switch arr := v.(type) {
	case []string:
		return arr
	case []interface{}:
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
	case primitive.A:
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
	}

	return result
}

// extractSecurityGroupIDs 从 security_groups 属性提取安全组ID列表
// 支持 []interface{}, primitive.A 等类型，每个元素可能是 string 或 map
func extractSecurityGroupIDs(v interface{}) []string {
	if v == nil {
		return nil
	}

	var result []string
	var items []interface{}

	switch arr := v.(type) {
	case []interface{}:
		items = arr
	case primitive.A:
		items = arr
	default:
		return nil
	}

	for _, item := range items {
		var sgID string
		switch sg := item.(type) {
		case string:
			sgID = sg
		case map[string]interface{}:
			// JSON 字段名是小写 "id"
			if id, ok := sg["id"].(string); ok {
				sgID = id
			} else if id, ok := sg["ID"].(string); ok {
				sgID = id
			}
		case primitive.M:
			if id, ok := sg["id"].(string); ok {
				sgID = id
			} else if id, ok := sg["ID"].(string); ok {
				sgID = id
			}
		}
		if sgID != "" {
			result = append(result, sgID)
		}
	}

	return result
}
