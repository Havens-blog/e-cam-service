// Package executor 任务执行器 - CDN/WAF资源同步
package executor

import (
	"context"
	"fmt"

	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncRegionCDN 同步单个地域的 CDN 加速域名
func (e *SyncAssetsExecutor) syncRegionCDN(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_cdn", account.Provider)

	cdnAdapter := adapter.CDN()
	if cdnAdapter == nil {
		return 0, fmt.Errorf("CDN适配器不可用")
	}

	cloudInstances, err := cdnAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取CDN域名列表失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		id := inst.DomainName
		if id == "" {
			id = inst.DomainID
		}
		cloudAssetIDSet[id] = true
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
			e.logger.Error("删除过期CDN域名失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期CDN域名", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertCDNToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存CDN域名失败", elog.String("domain", inst.DomainName), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域CDN完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// syncRegionWAF 同步单个地域的 WAF 实例
func (e *SyncAssetsExecutor) syncRegionWAF(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_waf", account.Provider)

	wafAdapter := adapter.WAF()
	if wafAdapter == nil {
		return 0, fmt.Errorf("WAF适配器不可用")
	}

	cloudInstances, err := wafAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取WAF实例列表失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
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
			e.logger.Error("删除过期WAF实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期WAF实例", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertWAFToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存WAF实例失败", elog.String("instance_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域WAF完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// convertCDNToInstance 将 CDN 域名转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertCDNToInstance(inst types.CDNInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_cdn", account.Provider)

	assetID := inst.DomainName
	if assetID == "" {
		assetID = inst.DomainID
	}

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "provider": inst.Provider,
		"domain_id": inst.DomainID, "domain_name": inst.DomainName, "cname": inst.Cname,
		"business_type": inst.BusinessType, "service_area": inst.ServiceArea,
		"origins": inst.Origins, "origin_type": inst.OriginType, "origin_host": inst.OriginHost,
		"https_enabled": inst.HTTPSEnabled, "cert_name": inst.CertName, "http2_enabled": inst.HTTP2Enabled,
		"bandwidth": inst.Bandwidth, "traffic_total": inst.TrafficTotal,
		"creation_time": inst.CreationTime, "modified_time": inst.ModifiedTime,
		"project_id": inst.ProjectID, "resource_group_id": inst.ResourceGroupID,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags, "description": inst.Description,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: assetID, AssetName: inst.DomainName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}

// convertWAFToInstance 将 WAF 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertWAFToInstance(inst types.WAFInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_waf", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "provider": inst.Provider,
		"edition":      inst.Edition,
		"domain_count": inst.DomainCount, "domain_limit": inst.DomainLimit,
		"protected_hosts": inst.ProtectedHosts,
		"rule_count":      inst.RuleCount, "acl_rule_count": inst.ACLRuleCount,
		"cc_rule_count": inst.CCRuleCount, "rate_limit_count": inst.RateLimitCount,
		"waf_enabled": inst.WAFEnabled, "cc_enabled": inst.CCEnabled, "anti_bot_enabled": inst.AntiBotEnabled,
		"qps": inst.QPS, "bandwidth": inst.Bandwidth,
		"exclusive_ip": inst.ExclusiveIP, "pay_type": inst.PayType,
		"creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"project_id": inst.ProjectID, "resource_group_id": inst.ResourceGroupID,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags, "description": inst.Description,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}
