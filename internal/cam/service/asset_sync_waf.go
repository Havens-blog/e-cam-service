package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func (s *assetSyncService) syncWAFInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_waf", account.Provider)

	wafAdapter := adapter.WAF()
	if wafAdapter == nil {
		s.logger.Warn("WAF适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := wafAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取WAF失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.InstanceID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"cloud_account_name": account.Name,
			"instance_id":        inst.InstanceID, "instance_name": inst.InstanceName,
			"status": inst.Status, "region": inst.Region,
			"edition": inst.Edition, "domain_count": inst.DomainCount,
			"domain_limit": inst.DomainLimit, "rule_count": inst.RuleCount,
			"acl_rule_count": inst.ACLRuleCount, "cc_rule_count": inst.CCRuleCount,
			"rate_limit_count": inst.RateLimitCount,
			"waf_enabled":      inst.WAFEnabled, "cc_enabled": inst.CCEnabled,
			"anti_bot_enabled": inst.AntiBotEnabled,
			"qps":              inst.QPS, "bandwidth": inst.Bandwidth,
			"exclusive_ip": inst.ExclusiveIP, "pay_type": inst.PayType,
			"creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
			"resource_group_id": inst.ResourceGroupID,
			"protected_hosts":   inst.ProtectedHosts, "source_ips": inst.SourceIPs,
			"cname": inst.Cname,
			"tags":  inst.Tags, "description": inst.Description,
		}
		assetName := inst.InstanceName
		if assetName == "" {
			assetName = inst.InstanceID
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_waf", account.Provider), AssetID: inst.InstanceID, AssetName: assetName,
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
