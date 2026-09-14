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

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		id := inst.DomainName
		if id == "" {
			id = inst.DomainID
		}
		items = append(items, syncItem{
			AssetID: id,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertCDNToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域CDN完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertCDNToInstance 将 CDN 域名转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertCDNToInstance(inst types.CDNInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_cdn", account.Provider)

	// 枚举归一化(业务类型/服务区域/状态),原始值保留到 *_raw
	cloudx.NormalizeCDNInstance(&inst)

	assetID := inst.DomainName
	if assetID == "" {
		assetID = inst.DomainID
	}

	attributes := map[string]any{
		"status":      inst.Status,
		"status_raw":  inst.StatusRaw,
		"region":      inst.Region,
		"provider":    inst.Provider,
		"description": inst.Description,

		"domain_id":         inst.DomainID,
		"domain_name":       inst.DomainName,
		"cname":             inst.Cname,
		"business_type":     inst.BusinessType,
		"business_type_raw": inst.BusinessTypeRaw,
		"service_area":      inst.ServiceArea,
		"service_area_raw":  inst.ServiceAreaRaw,

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
