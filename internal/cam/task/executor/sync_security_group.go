package executor

import (
	"context"
	"fmt"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncRegionSecurityGroup 同步单个地域的安全组
func (e *SyncAssetsExecutor) syncRegionSecurityGroup(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_security_group", account.Provider)

	e.logger.Info("开始同步安全组",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	sgAdapter := adapter.SecurityGroup()
	if sgAdapter == nil {
		e.logger.Warn("SecurityGroup适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := sgAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取安全组列表失败: %w", err)
	}

	e.logger.Info("获取到云端安全组",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	// 为每个安全组获取规则详情
	for i := range cloudInstances {
		sg := &cloudInstances[i]
		e.logger.Info("处理安全组",
			elog.String("sg_id", sg.SecurityGroupID),
			elog.String("sg_name", sg.SecurityGroupName),
			elog.Int("existing_ingress", len(sg.IngressRules)),
			elog.Int("existing_egress", len(sg.EgressRules)))

		// 始终尝试获取规则，不管是否已有规则
		rules, err := sgAdapter.GetSecurityGroupRules(ctx, region, sg.SecurityGroupID)
		if err != nil {
			e.logger.Warn("获取安全组规则失败",
				elog.String("sg_id", sg.SecurityGroupID),
				elog.FieldErr(err))
			continue
		}

		e.logger.Info("获取到安全组规则",
			elog.String("sg_id", sg.SecurityGroupID),
			elog.Int("rules_count", len(rules)))

		// 清空现有规则，重新填充
		sg.IngressRules = nil
		sg.EgressRules = nil

		for _, rule := range rules {
			e.logger.Debug("规则详情",
				elog.String("sg_id", sg.SecurityGroupID),
				elog.String("direction", rule.Direction),
				elog.String("protocol", rule.Protocol),
				elog.String("port_range", rule.PortRange))

			if rule.Direction == "ingress" {
				sg.IngressRules = append(sg.IngressRules, rule)
			} else {
				sg.EgressRules = append(sg.EgressRules, rule)
			}
		}
		sg.IngressRuleCount = len(sg.IngressRules)
		sg.EgressRuleCount = len(sg.EgressRules)

		e.logger.Info("安全组规则处理完成",
			elog.String("sg_id", sg.SecurityGroupID),
			elog.Int("ingress_count", sg.IngressRuleCount),
			elog.Int("egress_count", sg.EgressRuleCount))
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.SecurityGroupID] = true
	}

	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := e.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			e.logger.Error("删除过期安全组失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期安全组", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for i := range cloudInstances {
		inst := &cloudInstances[i]
		instance := e.convertSecurityGroupToInstance(*inst, account)

		e.logger.Info("保存安全组",
			elog.String("sg_id", inst.SecurityGroupID),
			elog.Int("ingress_rules", len(inst.IngressRules)),
			elog.Int("egress_rules", len(inst.EgressRules)))

		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存安全组失败", elog.String("asset_id", inst.SecurityGroupID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域安全组完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertSecurityGroupToInstance 将安全组转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertSecurityGroupToInstance(inst types.SecurityGroupInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_security_group", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"region":              inst.Region,
		"provider":            inst.Provider,
		"description":         inst.Description,
		"security_group_type": inst.SecurityGroupType,

		// 网络信息
		"vpc_id":   inst.VPCID,
		"vpc_name": inst.VPCName,

		// 规则统计
		"ingress_rule_count": inst.IngressRuleCount,
		"egress_rule_count":  inst.EgressRuleCount,

		// 关联实例
		"instance_count": inst.InstanceCount,
		"instance_ids":   inst.InstanceIDs,

		// 规则详情
		"ingress_rules": inst.IngressRules,
		"egress_rules":  inst.EgressRules,

		// 资源组
		"resource_group_id": inst.ResourceGroupID,

		// 时间信息
		"creation_time": inst.CreationTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.SecurityGroupID,
		AssetName:  inst.SecurityGroupName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
