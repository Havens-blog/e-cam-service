package web

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GetECSRelations 获取ECS实例关联资源
// @Summary 获取ECS实例关联资源
// @Description 获取指定ECS实例关联的云盘、快照、安全组、VPC、子网等资源
// @Tags 资产管理-ECS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ECSRelationsResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/ecs/{asset_id}/relations [get]
func (h *AssetHandler) GetECSRelations(ctx *gin.Context) {
	assetID := ctx.Param("asset_id")
	if assetID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.ParamsError, "asset_id is required"))
		return
	}

	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")

	h.logger.Info("GetECSRelations 请求参数",
		elog.String("asset_id", assetID),
		elog.Int64("tenant_id", tenantID),
		elog.String("provider", provider))

	// 1. 先获取 ECS 实例
	ecsFilter := domain.InstanceFilter{
		AssetID:  assetID,
		TenantID: tenantID,
		Provider: provider,
		Limit:    10,
	}

	ecsInstances, _, err := h.instanceSvc.List(ctx.Request.Context(), ecsFilter)
	if err != nil {
		h.logger.Error("查询ECS实例失败", elog.FieldErr(err))
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	h.logger.Info("查询ECS实例结果",
		elog.Int("count", len(ecsInstances)))
	for _, inst := range ecsInstances {
		h.logger.Info("ECS实例",
			elog.String("model_uid", inst.ModelUID),
			elog.String("asset_id", inst.AssetID),
			elog.Int64("tenant_id", inst.TenantID),
			elog.Int64("account_id", inst.AccountID))
	}

	// 找到匹配的 ECS 实例
	var ecsInstance *domain.Instance
	for i, inst := range ecsInstances {
		if matchAssetType(inst.ModelUID, "ecs") {
			ecsInstance = &ecsInstances[i]
			break
		}
	}

	if ecsInstance == nil {
		// 如果带 provider 查不到，尝试不带 provider 查询
		if provider != "" {
			h.logger.Warn("带provider查不到ECS实例，尝试不带provider查询",
				elog.String("asset_id", assetID))
			ecsFilter.Provider = ""
			ecsInstances, _, err = h.instanceSvc.List(ctx.Request.Context(), ecsFilter)
			if err == nil {
				for i, inst := range ecsInstances {
					if matchAssetType(inst.ModelUID, "ecs") {
						ecsInstance = &ecsInstances[i]
						break
					}
				}
			}
		}
		if ecsInstance == nil {
			ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
			return
		}
	}

	// 从 ECS 实例属性中获取 region 和 account_id
	region := ""
	if r, ok := ecsInstance.Attributes["region"].(string); ok {
		region = r
	}
	accountID := ecsInstance.AccountID
	// 使用 ECS 实例自身的 provider（更可靠）
	instanceProvider := ""
	if p, ok := ecsInstance.Attributes["provider"].(string); ok {
		instanceProvider = p
	}

	h.logger.Info("ECS实例详情",
		elog.String("model_uid", ecsInstance.ModelUID),
		elog.String("region", region),
		elog.Int64("account_id", accountID),
		elog.String("instance_provider", instanceProvider),
		elog.Any("security_group_ids", ecsInstance.Attributes["security_group_ids"]),
		elog.Any("security_groups", ecsInstance.Attributes["security_groups"]))

	// 获取 VPC ID 和 子网 ID
	vpcID := ""
	subnetID := ""
	if v, ok := ecsInstance.Attributes["vpc_id"].(string); ok {
		vpcID = v
	}
	// 子网字段可能是 vswitch_id (阿里云) 或 subnet_id (其他云)
	if s, ok := ecsInstance.Attributes["vswitch_id"].(string); ok {
		subnetID = s
	} else if s, ok := ecsInstance.Attributes["subnet_id"].(string); ok {
		subnetID = s
	}

	// 2. 查询关联的云盘 (通过 instance_id 属性)
	diskFilter := domain.InstanceFilter{
		ModelUID:  "disk",
		TenantID:  tenantID,
		AccountID: accountID,
		Attributes: map[string]interface{}{
			"instance_id": assetID,
		},
		Limit: 100,
	}
	if region != "" {
		diskFilter.Attributes["region"] = region
	}

	disks, _, _ := h.instanceSvc.List(ctx.Request.Context(), diskFilter)
	h.logger.Info("查询关联云盘结果", elog.Int("count", len(disks)))

	// 3. 查询关联的快照 (通过云盘ID查询)
	var snapshots []domain.Instance
	for _, disk := range disks {
		snapshotFilter := domain.InstanceFilter{
			ModelUID:  "snapshot",
			TenantID:  tenantID,
			AccountID: accountID,
			Attributes: map[string]interface{}{
				"source_disk_id": disk.AssetID,
			},
			Limit: 100,
		}
		if region != "" {
			snapshotFilter.Attributes["region"] = region
		}
		diskSnapshots, _, _ := h.instanceSvc.List(ctx.Request.Context(), snapshotFilter)
		snapshots = append(snapshots, diskSnapshots...)
	}
	// 如果通过磁盘没找到快照，尝试通过 source_instance_id 直接查询
	if len(snapshots) == 0 {
		snapshotByInstFilter := domain.InstanceFilter{
			ModelUID:  "snapshot",
			TenantID:  tenantID,
			AccountID: accountID,
			Attributes: map[string]interface{}{
				"source_instance_id": assetID,
			},
			Limit: 100,
		}
		if region != "" {
			snapshotByInstFilter.Attributes["region"] = region
		}
		snapshots, _, _ = h.instanceSvc.List(ctx.Request.Context(), snapshotByInstFilter)
	}
	h.logger.Info("查询关联快照结果", elog.Int("count", len(snapshots)))

	// 4. 查询关联的安全组 (通过 ECS 的 security_group_ids 或 security_groups 属性)
	var securityGroups []domain.Instance
	var sgIDs []string

	// 从 security_group_ids 提取 (支持 []interface{} 和 primitive.A)
	sgIDs = extractStringArray(ecsInstance.Attributes["security_group_ids"])

	// 如果 security_group_ids 为空，尝试从 security_groups 提取
	if len(sgIDs) == 0 {
		sgIDs = extractSecurityGroupIDs(ecsInstance.Attributes["security_groups"])
	}

	h.logger.Info("提取安全组ID", elog.Any("sg_ids", sgIDs))

	// 根据安全组 ID 查询安全组实例
	for _, sgID := range sgIDs {
		sgFilter := domain.InstanceFilter{
			ModelUID:  "security_group",
			AssetID:   sgID,
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     10,
		}
		sgInstances, _, sgErr := h.instanceSvc.List(ctx.Request.Context(), sgFilter)
		h.logger.Info("查询安全组",
			elog.String("sg_id", sgID),
			elog.Int("result_count", len(sgInstances)),
			elog.FieldErr(sgErr))
		if len(sgInstances) == 0 {
			// 回退: 不带 AccountID 查询
			sgFilter2 := domain.InstanceFilter{
				ModelUID: "security_group",
				AssetID:  sgID,
				TenantID: tenantID,
				Limit:    10,
			}
			sgInstances, _, sgErr = h.instanceSvc.List(ctx.Request.Context(), sgFilter2)
			h.logger.Info("回退查询安全组(不带account_id)",
				elog.String("sg_id", sgID),
				elog.Int("result_count", len(sgInstances)),
				elog.FieldErr(sgErr))
		}
		for _, inst := range sgInstances {
			if matchAssetType(inst.ModelUID, "security_group") {
				securityGroups = append(securityGroups, inst)
				break
			}
		}
	}
	h.logger.Info("查询关联安全组结果", elog.Int("count", len(securityGroups)))

	// 如果数据库中没有找到安全组，从 ECS 实例属性中构建基本信息
	if len(securityGroups) == 0 && len(sgIDs) > 0 {
		h.logger.Info("安全组未在数据库中找到，从ECS属性构建基本信息")
		sgList := ecsInstance.Attributes["security_groups"]

		// 统一转换为 []interface{}
		var sgItems []interface{}
		switch arr := sgList.(type) {
		case []interface{}:
			sgItems = arr
		case primitive.A:
			sgItems = arr
		}

		for _, item := range sgItems {
			sgID := ""
			sgName := ""
			sgDesc := ""

			switch sgMap := item.(type) {
			case map[string]interface{}:
				if id, ok := sgMap["id"].(string); ok {
					sgID = id
				}
				if name, ok := sgMap["name"].(string); ok {
					sgName = name
				}
				if desc, ok := sgMap["description"].(string); ok {
					sgDesc = desc
				}
			case primitive.M:
				if id, ok := sgMap["id"].(string); ok {
					sgID = id
				}
				if name, ok := sgMap["name"].(string); ok {
					sgName = name
				}
				if desc, ok := sgMap["description"].(string); ok {
					sgDesc = desc
				}
			}

			if sgID != "" {
				securityGroups = append(securityGroups, domain.Instance{
					ModelUID:  instanceProvider + "_security_group",
					AssetID:   sgID,
					AssetName: sgName,
					TenantID:  tenantID,
					AccountID: accountID,
					Attributes: map[string]interface{}{
						"provider":    instanceProvider,
						"region":      region,
						"description": sgDesc,
						"_from_ecs":   true,
					},
				})
			}
		}
		h.logger.Info("从ECS属性构建安全组", elog.Int("count", len(securityGroups)))
	}

	// 5. 查询关联的 VPC
	var vpcInstance *domain.Instance
	if vpcID != "" {
		vpcFilter := domain.InstanceFilter{
			ModelUID:  "vpc",
			AssetID:   vpcID,
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     10,
		}
		vpcInstances, _, _ := h.instanceSvc.List(ctx.Request.Context(), vpcFilter)
		h.logger.Info("查询关联VPC", elog.String("vpc_id", vpcID), elog.Int("result_count", len(vpcInstances)))
		for i, inst := range vpcInstances {
			if matchAssetType(inst.ModelUID, "vpc") {
				vpcInstance = &vpcInstances[i]
				break
			}
		}
	}

	// 6. 查询关联的子网/交换机
	var subnetInstance *domain.Instance
	if subnetID != "" {
		subnetFilter := domain.InstanceFilter{
			ModelUID:  "subnet",
			AssetID:   subnetID,
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     10,
		}
		subnetInstances, _, _ := h.instanceSvc.List(ctx.Request.Context(), subnetFilter)
		h.logger.Info("查询关联子网", elog.String("subnet_id", subnetID), elog.Int("result_count", len(subnetInstances)))
		for i, inst := range subnetInstances {
			if matchAssetType(inst.ModelUID, "subnet") || matchAssetType(inst.ModelUID, "vswitch") {
				subnetInstance = &subnetInstances[i]
				break
			}
		}

		// 子网兜底: 如果数据库中没有子网数据，从 ECS 实例属性中构建基本信息
		if subnetInstance == nil {
			h.logger.Info("子网未在数据库中找到，从ECS属性构建基本信息",
				elog.String("subnet_id", subnetID))
			subnetName := ""
			if n, ok := ecsInstance.Attributes["vswitch_name"].(string); ok {
				subnetName = n
			}
			if subnetName == "" {
				if n, ok := ecsInstance.Attributes["subnet_name"].(string); ok {
					subnetName = n
				}
			}
			zone := ""
			if z, ok := ecsInstance.Attributes["zone"].(string); ok {
				zone = z
			}
			fallbackSubnet := domain.Instance{
				ModelUID:  instanceProvider + "_subnet",
				AssetID:   subnetID,
				AssetName: subnetName,
				TenantID:  tenantID,
				AccountID: accountID,
				Attributes: map[string]interface{}{
					"provider":  instanceProvider,
					"region":    region,
					"zone":      zone,
					"vpc_id":    vpcID,
					"_from_ecs": true,
				},
			}
			subnetInstance = &fallbackSubnet
		}
	}

	// 7. 构建响应
	ecsVO := h.toUnifiedAssetVO(*ecsInstance)
	resp := ECSRelationsResp{
		ECS:            &ecsVO,
		Disks:          h.toUnifiedAssetVOs(disks),
		Snapshots:      h.toUnifiedAssetVOs(snapshots),
		SecurityGroups: h.toUnifiedAssetVOs(securityGroups),
	}

	if vpcInstance != nil {
		vpcVO := h.toUnifiedAssetVO(*vpcInstance)
		resp.VPC = &vpcVO
	}
	if subnetInstance != nil {
		subnetVO := h.toUnifiedAssetVO(*subnetInstance)
		resp.Subnet = &subnetVO
	}

	ctx.JSON(200, Result(resp))
}
