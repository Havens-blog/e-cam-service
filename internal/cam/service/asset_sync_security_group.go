package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func (s *assetSyncService) syncSecurityGroupInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_security_group", account.Provider)

	sgAdapter := adapter.SecurityGroup()
	if sgAdapter == nil {
		s.logger.Warn("SecurityGroup适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := sgAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取安全组失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.SecurityGroupID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region":              inst.Region,
			"security_group_id":   inst.SecurityGroupID,
			"security_group_name": inst.SecurityGroupName,
			"security_group_type": inst.SecurityGroupType,
			"vpc_id":              inst.VPCID, "vpc_name": inst.VPCName,
			"ingress_rule_count": inst.IngressRuleCount,
			"egress_rule_count":  inst.EgressRuleCount,
			"instance_count":     inst.InstanceCount,
			"instance_ids":       inst.InstanceIDs,
			"resource_group_id":  inst.ResourceGroupID,
			"creation_time":      inst.CreationTime,
			"tags":               inst.Tags, "description": inst.Description,
		}
		assetName := inst.SecurityGroupName
		if assetName == "" {
			assetName = inst.SecurityGroupID
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_security_group", account.Provider), AssetID: inst.SecurityGroupID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}
