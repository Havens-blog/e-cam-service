package tencent

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	teo "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/teo/v20220901"
	waf "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/waf/v20180125"
)

// WAFAdapter 腾讯云WAF适配器
// 同步 WAF 防护域名 + EdgeOne(EO) 站点，统一归类为 WAF 防护资源
type WAFAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewWAFAdapter 创建WAF适配器
func NewWAFAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *WAFAdapter {
	return &WAFAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createWAFClient 创建WAF客户端
func (a *WAFAdapter) createWAFClient(region string) (*waf.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	credential := common.NewCredential(a.accessKeyID, a.accessKeySecret)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "waf.tencentcloudapi.com"
	return waf.NewClient(credential, region, cpf)
}

// createTEOClient 创建 EdgeOne 客户端
func (a *WAFAdapter) createTEOClient() (*teo.Client, error) {
	credential := common.NewCredential(a.accessKeyID, a.accessKeySecret)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "teo.tencentcloudapi.com"
	// TEO 是全局服务，region 不影响
	return teo.NewClient(credential, "", cpf)
}

// ListInstances 获取WAF防护域名 + EO站点列表
func (a *WAFAdapter) ListInstances(ctx context.Context, region string) ([]types.WAFInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个WAF防护域名详情
func (a *WAFAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.WAFInstance, error) {
	instances, err := a.ListInstances(ctx, region)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.InstanceID == instanceID {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("WAF防护资源不存在: %s", instanceID)
}

// ListInstancesByIDs 批量获取WAF防护域名
func (a *WAFAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.WAFInstance, error) {
	var result []types.WAFInstance
	for _, id := range instanceIDs {
		inst, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取WAF防护资源失败", elog.String("instance_id", id), elog.FieldErr(err))
			continue
		}
		result = append(result, *inst)
	}
	return result, nil
}

// GetInstanceStatus 获取防护域名状态
func (a *WAFAdapter) GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error) {
	inst, err := a.GetInstance(ctx, region, instanceID)
	if err != nil {
		return "", err
	}
	return inst.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取WAF防护域名 + EO站点列表
// WAF DescribeDomains 和 EO DescribeZones 都是全局服务，不按 region 区分
func (a *WAFAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	var allInstances []types.WAFInstance

	// 1. 查询 WAF 防护域名（DescribeDomains）
	// WAF API 只支持 ap-guangzhou 地域，固定使用该 region
	wafDomains, err := a.listWAFDomains(ctx, "ap-guangzhou", filter)
	if err != nil {
		a.logger.Warn("获取腾讯云WAF防护域名失败", elog.FieldErr(err))
	} else {
		allInstances = append(allInstances, wafDomains...)
	}

	// 2. 查询 EdgeOne(EO) 站点（DescribeZones）— 全局服务
	eoZones, err := a.listEOZones(ctx, filter)
	if err != nil {
		a.logger.Warn("获取腾讯云EO站点失败", elog.FieldErr(err))
	} else {
		allInstances = append(allInstances, eoZones...)
	}

	a.logger.Info("获取腾讯云WAF防护资源列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}

// listWAFDomains 查询 WAF 防护域名
func (a *WAFAdapter) listWAFDomains(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	client, err := a.createWAFClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建WAF客户端失败: %w", err)
	}

	var allInstances []types.WAFInstance
	offset := uint64(0)
	limit := uint64(100)

	for {
		request := waf.NewDescribeDomainsRequest()
		request.Offset = &offset
		request.Limit = &limit

		response, err := client.DescribeDomains(request)
		if err != nil {
			return nil, fmt.Errorf("获取WAF防护域名列表失败: %w", err)
		}

		if response.Response.Domains == nil || len(response.Response.Domains) == 0 {
			break
		}

		for _, d := range response.Response.Domains {
			allInstances = append(allInstances, a.convertWAFDomainToInstance(d, region))
		}

		total := uint64(0)
		if response.Response.Total != nil {
			total = *response.Response.Total
		}
		if uint64(len(allInstances)) >= total || len(response.Response.Domains) < int(limit) {
			break
		}
		offset += limit
	}

	return allInstances, nil
}

// listEOZones 查询 EdgeOne 站点
func (a *WAFAdapter) listEOZones(ctx context.Context, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	client, err := a.createTEOClient()
	if err != nil {
		return nil, fmt.Errorf("创建TEO客户端失败: %w", err)
	}

	var allInstances []types.WAFInstance
	offset := int64(0)
	limit := int64(100)

	for {
		request := teo.NewDescribeZonesRequest()
		request.Offset = &offset
		request.Limit = &limit

		response, err := client.DescribeZones(request)
		if err != nil {
			return nil, fmt.Errorf("获取EO站点列表失败: %w", err)
		}

		if response.Response.Zones == nil || len(response.Response.Zones) == 0 {
			break
		}

		for _, zone := range response.Response.Zones {
			allInstances = append(allInstances, a.convertEOZoneToInstance(zone))
		}

		total := int64(0)
		if response.Response.TotalCount != nil {
			total = *response.Response.TotalCount
		}
		if int64(len(allInstances)) >= total || len(response.Response.Zones) < int(limit) {
			break
		}
		offset += limit
	}

	return allInstances, nil
}

// convertWAFDomainToInstance 将腾讯云 WAF 防护域名转换为通用 WAFInstance
func (a *WAFAdapter) convertWAFDomainToInstance(d *waf.DomainInfo, region string) types.WAFInstance {
	domain := ""
	if d.Domain != nil {
		domain = *d.Domain
	}
	domainID := ""
	if d.DomainId != nil {
		domainID = *d.DomainId
	}
	edition := ""
	if d.Edition != nil {
		edition = *d.Edition
	}

	status := "active"
	wafEnabled := true
	// State: 0=未防护 1=防护中
	if d.State != nil && *d.State == 0 {
		status = "suspended"
		wafEnabled = false
	}

	// 提取源站 IP
	var sourceIPs []string
	if d.SrcList != nil {
		for _, ip := range d.SrcList {
			if ip != nil && *ip != "" {
				sourceIPs = append(sourceIPs, *ip)
			}
		}
	}

	cname := ""
	if d.Cname != nil {
		cname = *d.Cname
	}

	return types.WAFInstance{
		InstanceID:     domainID,
		InstanceName:   domain,
		Status:         status,
		Region:         region,
		Edition:        edition,
		DomainCount:    1,
		ProtectedHosts: []string{domain},
		SourceIPs:      sourceIPs,
		Cname:          cname,
		WAFEnabled:     wafEnabled,
		Provider:       "tencent",
		Description:    "WAF防护域名",
		Tags:           make(map[string]string),
	}
}

// convertEOZoneToInstance 将腾讯云 EdgeOne 站点转换为通用 WAFInstance
func (a *WAFAdapter) convertEOZoneToInstance(zone *teo.Zone) types.WAFInstance {
	zoneID := ""
	if zone.ZoneId != nil {
		zoneID = *zone.ZoneId
	}
	zoneName := ""
	if zone.ZoneName != nil {
		zoneName = *zone.ZoneName
	}
	status := "active"
	if zone.Status != nil {
		// active / paused / deleted
		status = *zone.Status
	}
	area := ""
	if zone.Area != nil {
		area = *zone.Area
	}
	zoneType := ""
	if zone.Type != nil {
		zoneType = *zone.Type
	}
	createdOn := ""
	if zone.CreatedOn != nil {
		createdOn = *zone.CreatedOn
	}
	modifiedOn := ""
	if zone.ModifiedOn != nil {
		modifiedOn = *zone.ModifiedOn
	}

	return types.WAFInstance{
		InstanceID:     zoneID,
		InstanceName:   zoneName,
		Status:         status,
		Edition:        "EdgeOne",
		DomainCount:    1,
		ProtectedHosts: []string{zoneName},
		WAFEnabled:     status == "active",
		Provider:       "tencent",
		CreationTime:   createdOn,
		Description:    fmt.Sprintf("EdgeOne站点 type=%s area=%s modified=%s", zoneType, area, modifiedOn),
		Tags:           make(map[string]string),
	}
}
