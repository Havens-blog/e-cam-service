package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func (s *assetSyncService) syncCDNInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_cdn", account.Provider)

	cdnAdapter := adapter.CDN()
	if cdnAdapter == nil {
		s.logger.Warn("CDN适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := cdnAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取CDN失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		assetID := inst.DomainName
		if inst.DomainID != "" {
			assetID = inst.DomainID
		}
		cloudAssetIDs[assetID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, "", cloudAssetIDs)

	for _, inst := range instances {
		// 提取源站地址列表（用于拓扑链路匹配）
		var originAddrs []string
		for _, o := range inst.Origins {
			if o.Address != "" {
				originAddrs = append(originAddrs, o.Address)
			}
		}

		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"domain_id": inst.DomainID, "domain_name": inst.DomainName,
			"cname": inst.Cname, "status": inst.Status,
			"region": inst.Region, "business_type": inst.BusinessType,
			"service_area": inst.ServiceArea, "origin_type": inst.OriginType,
			"origin_host": inst.OriginHost, "https_enabled": inst.HTTPSEnabled,
			"cert_name": inst.CertName, "http2_enabled": inst.HTTP2Enabled,
			"bandwidth": inst.Bandwidth, "traffic_total": inst.TrafficTotal,
			"creation_time": inst.CreationTime, "modified_time": inst.ModifiedTime,
			"resource_group_id": inst.ResourceGroupID,
			"origins":           originAddrs, // 回源地址列表，用于拓扑链路级联匹配
			"tags":              inst.Tags, "description": inst.Description,
		}
		assetID := inst.DomainName
		if inst.DomainID != "" {
			assetID = inst.DomainID
		}
		assetName := inst.DomainName
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_cdn", account.Provider), AssetID: assetID, AssetName: assetName,
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
