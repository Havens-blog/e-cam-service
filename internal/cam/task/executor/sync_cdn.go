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

	e.logger.Info("同步地域CDN完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertCDNToInstance 将 CDN 域名转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertCDNToInstance(inst types.CDNInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_cdn", account.Provider)

	assetID := inst.DomainName
	if assetID == "" {
		assetID = inst.DomainID
	}

	attributes := map[string]any{
		"status":      inst.Status,
		"region":      inst.Region,
		"provider":    inst.Provider,
		"description": inst.Description,

		"domain_id":     inst.DomainID,
		"domain_name":   inst.DomainName,
		"cname":         inst.Cname,
		"business_type": inst.BusinessType,
		"service_area":  inst.ServiceArea,

		"origins":     inst.Origins,
		"origin_type": inst.OriginType,
		"origin_host": inst.OriginHost,

		"https_enabled": inst.HTTPSEnabled,
		"cert_name":     inst.CertName,
		"http2_enabled": inst.HTTP2Enabled,

		"bandwidth":     inst.Bandwidth,
		"traffic_total": inst.TrafficTotal,
		"creation_time": inst.CreationTime,
		"modified_time": inst.ModifiedTime,

		"project_id":        inst.ProjectID,
		"resource_group_id": inst.ResourceGroupID,

		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,
		"tags":               inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    assetID,
		AssetName:  inst.DomainName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
