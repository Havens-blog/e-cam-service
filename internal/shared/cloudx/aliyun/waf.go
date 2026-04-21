package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/gotomicro/ego/core/elog"
)

// WAFAdapter 阿里云WAF适配器
// 同步的是 Web 应用防火墙的防护域名/防护对象（DescribeDefenseResources），不是 WAF 实例
type WAFAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
	instanceID      string // 缓存的 WAF 实例 ID
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

// createClient 创建WAF客户端
func (a *WAFAdapter) createClient(region string) (*sdk.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	return sdk.NewClientWithAccessKey(region, a.accessKeyID, a.accessKeySecret)
}

// wafEndpoint 根据地域返回 WAF 3.0 API 端点
func (a *WAFAdapter) wafEndpoint(region string) string {
	if region == "ap-southeast-1" || region == "" {
		return "wafopenapi.ap-southeast-1.aliyuncs.com"
	}
	return "wafopenapi.cn-hangzhou.aliyuncs.com"
}

// describeDefenseResourcesResponse 阿里云 WAF 3.0 DescribeDefenseResources 响应
type describeDefenseResourcesResponse struct {
	RequestID  string            `json:"RequestId"`
	TotalCount int64             `json:"TotalCount"`
	Resources  []defenseResource `json:"Resources"`
}

// defenseResource 防护对象（域名）
type defenseResource struct {
	Resource        string          `json:"Resource"`        // 防护对象名称（域名）
	Description     string          `json:"Description"`     // 描述
	Pattern         string          `json:"Pattern"`         // 防护模式: domain
	GmtCreate       int64           `json:"GmtCreate"`       // 创建时间（毫秒时间戳）
	GmtModified     int64           `json:"GmtModified"`     // 修改时间
	OwnerUserId     string          `json:"OwnerUserId"`     // 所属用户ID
	AcwCookieStatus int             `json:"AcwCookieStatus"` // Cookie 状态
	CustomHeaders   []any           `json:"CustomHeaders"`
	Detail          json.RawMessage `json:"Detail"` // 详情（JSON 字符串）
}

// defenseResourceDetail 防护对象详情
type defenseResourceDetail struct {
	Cname            string   `json:"Cname"`
	HttpPorts        []int    `json:"HttpPorts"`
	HttpsPorts       []int    `json:"HttpsPorts"`
	Http2Ports       []int    `json:"Http2Ports"`
	ExclusiveIP      bool     `json:"ExclusiveIp"`
	IPV6Status       int      `json:"Ipv6Status"`
	ProtectionStatus int      `json:"ProtectionStatus"` // 0=关闭 1=开启
	Origins          []string `json:"Origins"`
}

// describeDomainDetailResponse 阿里云 WAF 3.0 DescribeDomainDetail 响应
// 注意：Redirect、Cname、Domain 等都是顶层字段，不是嵌套在 Domain 对象里
type describeDomainDetailResponse struct {
	RequestID string          `json:"RequestId"`
	Domain    string          `json:"Domain"`   // 域名
	Cname     string          `json:"Cname"`    // WAF 分配的 CNAME
	Status    int             `json:"Status"`   // 1=正常
	Listen    *listenDetail   `json:"Listen"`   // 监听配置
	Redirect  *redirectDetail `json:"Redirect"` // 回源配置（包含源站地址）
}

type listenDetail struct {
	HttpPorts  []int `json:"HttpPorts"`
	HttpsPorts []int `json:"HttpsPorts"`
	Http2Ports []int `json:"Http2Ports"`
}

type redirectDetail struct {
	Backends         []backendItem `json:"Backends"`    // 源站地址列表 [{"Backend":"xxx"}]
	AllBackends      []string      `json:"AllBackends"` // 所有源站地址（扁平列表）
	BackendList      []string      `json:"BackendList"` // 源站地址列表（扁平列表）
	Loadbalance      string        `json:"Loadbalance"` // 负载均衡算法
	FocusHttpBackend bool          `json:"FocusHttpBackend"`
}

type backendItem struct {
	Backend string `json:"Backend"` // 源站地址
}

// ListInstances 获取WAF防护域名列表
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
	return nil, fmt.Errorf("WAF防护域名不存在: %s", instanceID)
}

// ListInstancesByIDs 批量获取WAF防护域名
func (a *WAFAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.WAFInstance, error) {
	var result []types.WAFInstance
	for _, id := range instanceIDs {
		inst, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取WAF防护域名失败", elog.String("instance_id", id), elog.FieldErr(err))
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

// ListInstancesWithFilter 带过滤条件获取WAF防护域名列表
// 使用 WAF 3.0 DescribeDefenseResources API 查询防护对象
func (a *WAFAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	if region == "" {
		region = a.defaultRegion
	}

	// WAF 3.0 是全局服务，只需要调用一次（中国站用 cn-hangzhou，国际站用 ap-southeast-1）
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建WAF客户端失败: %w", err)
	}

	var allInstances []types.WAFInstance
	pageNumber := 1
	pageSize := 100

	if filter != nil && filter.PageSize > 0 {
		pageSize = filter.PageSize
	}

	for {
		request := requests.NewCommonRequest()
		request.Method = "POST"
		request.Scheme = "https"
		request.Domain = a.wafEndpoint(region)
		request.Version = "2021-10-01"
		request.ApiName = "DescribeDefenseResources"
		request.QueryParams["RegionId"] = region
		request.QueryParams["PageNumber"] = strconv.Itoa(pageNumber)
		request.QueryParams["PageSize"] = strconv.Itoa(pageSize)

		if filter != nil && filter.InstanceName != "" {
			// 按域名查询
			request.QueryParams["Query"] = fmt.Sprintf(`Resource LIKE "%s"`, filter.InstanceName)
		}

		response, err := client.ProcessCommonRequest(request)
		if err != nil {
			// WAF 可能未开通
			a.logger.Debug("获取WAF防护域名失败(可能未开通)", elog.String("region", region), elog.FieldErr(err))
			return nil, nil
		}

		var resp describeDefenseResourcesResponse
		if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
			return nil, fmt.Errorf("解析WAF防护域名响应失败: %w", err)
		}

		for _, res := range resp.Resources {
			inst := a.convertResourceToInstance(res, region)
			// Detail 中的 domain 字段才是真实域名，Resource 可能带后缀（如 xxx-waf）
			realDomain := extractDomainFromDetail(res.Detail)
			if realDomain == "" {
				realDomain = res.Resource
			}
			domainDetail, err := a.getDomainDetail(client, region, realDomain)
			if err != nil {
				a.logger.Warn("获取WAF域名详情失败，跳过源站信息",
					elog.String("resource", res.Resource),
					elog.String("domain", realDomain),
					elog.FieldErr(err))
			} else if domainDetail != nil {
				if domainDetail.Redirect != nil {
					// 优先用 AllBackends（扁平列表），回退到 Backends 提取
					if len(domainDetail.Redirect.AllBackends) > 0 {
						inst.SourceIPs = domainDetail.Redirect.AllBackends
					} else if len(domainDetail.Redirect.BackendList) > 0 {
						inst.SourceIPs = domainDetail.Redirect.BackendList
					} else {
						for _, b := range domainDetail.Redirect.Backends {
							if b.Backend != "" {
								inst.SourceIPs = append(inst.SourceIPs, b.Backend)
							}
						}
					}
				}
				if domainDetail.Cname != "" {
					inst.Cname = domainDetail.Cname
				}
			}
			allInstances = append(allInstances, inst)
		}

		if int64(len(allInstances)) >= resp.TotalCount || len(resp.Resources) < pageSize {
			break
		}
		pageNumber++
	}

	a.logger.Info("获取阿里云WAF防护域名列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}

// convertResourceToInstance 将阿里云 WAF 防护对象转换为通用 WAFInstance
func (a *WAFAdapter) convertResourceToInstance(res defenseResource, region string) types.WAFInstance {
	// 解析 Detail JSON
	var detail defenseResourceDetail
	if len(res.Detail) > 0 {
		// Detail 可能是 JSON 字符串或 JSON 对象
		var detailStr string
		if err := json.Unmarshal(res.Detail, &detailStr); err == nil {
			// Detail 是 JSON 字符串，需要二次解析
			_ = json.Unmarshal([]byte(detailStr), &detail)
		} else {
			// Detail 是 JSON 对象
			_ = json.Unmarshal(res.Detail, &detail)
		}
		// 打印前3个域名的 Detail 原始内容，帮助调试
		if false { // 调试完成，关闭详细日志
			a.logger.Info("阿里云WAF防护对象Detail",
				elog.String("resource", res.Resource),
				elog.String("detail_raw", string(res.Detail)),
				elog.Any("parsed_origins", detail.Origins),
				elog.String("parsed_cname", detail.Cname))
		}
	}

	status := "active"
	wafEnabled := true
	if detail.ProtectionStatus == 0 {
		status = "suspended"
		wafEnabled = false
	}

	httpsEnabled := len(detail.HttpsPorts) > 0
	exclusiveIP := detail.ExclusiveIP

	return types.WAFInstance{
		InstanceID:     res.Resource,
		InstanceName:   res.Resource,
		Status:         status,
		Region:         region,
		DomainCount:    1,
		ProtectedHosts: []string{res.Resource},
		SourceIPs:      detail.Origins,
		Cname:          detail.Cname,
		WAFEnabled:     wafEnabled,
		ExclusiveIP:    exclusiveIP,
		Provider:       "aliyun",
		Description:    fmt.Sprintf("%s https=%v", res.Description, httpsEnabled),
		Tags:           make(map[string]string),
	}
}

// getDomainDetail 获取阿里云 WAF 域名详情（包含源站信息）
// 使用 DescribeDomainDetail API，需要先获取 WAF 实例 ID
func (a *WAFAdapter) getDomainDetail(client *sdk.Client, region, domain string) (*describeDomainDetailResponse, error) {
	// 先获取 WAF 实例 ID
	instanceID, err := a.getInstanceID(client, region)
	if err != nil {
		return nil, fmt.Errorf("获取WAF实例ID失败: %w", err)
	}

	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = a.wafEndpoint(region)
	request.Version = "2021-10-01"
	request.ApiName = "DescribeDomainDetail"
	request.QueryParams["RegionId"] = region
	request.QueryParams["InstanceId"] = instanceID
	request.QueryParams["Domain"] = domain

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		return nil, fmt.Errorf("获取WAF域名详情失败: %w", err)
	}

	var resp describeDomainDetailResponse
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		return nil, fmt.Errorf("解析WAF域名详情响应失败: %w", err)
	}

	if resp.Domain == "" {
		return nil, nil
	}

	return &resp, nil
}

// describeInstanceResp 阿里云 WAF DescribeInstance 响应（仅用于获取实例ID）
type describeInstanceResp struct {
	InstanceID string `json:"InstanceId"`
}

// cachedInstanceID is no longer used — instance ID is cached on the adapter

// getInstanceID 获取阿里云 WAF 实例 ID
func (a *WAFAdapter) getInstanceID(client *sdk.Client, region string) (string, error) {
	if a.instanceID != "" {
		return a.instanceID, nil
	}

	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = a.wafEndpoint(region)
	request.Version = "2021-10-01"
	request.ApiName = "DescribeInstance"
	request.QueryParams["RegionId"] = region

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		return "", fmt.Errorf("获取WAF实例失败: %w", err)
	}

	var resp describeInstanceResp
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		return "", fmt.Errorf("解析WAF实例响应失败: %w", err)
	}

	if resp.InstanceID == "" {
		return "", fmt.Errorf("WAF实例ID为空")
	}

	a.instanceID = resp.InstanceID
	return a.instanceID, nil
}

// extractDomainFromDetail 从 Detail JSON 中提取真实域名
// Detail 格式: {"product":"waf","domain":"www.example.com"} 或 {"product":"clb7","instanceId":"lb-xxx",...}
func extractDomainFromDetail(detail json.RawMessage) string {
	if len(detail) == 0 {
		return ""
	}
	var d struct {
		Domain string `json:"domain"`
	}
	// Detail 可能是 JSON 字符串或 JSON 对象
	var detailStr string
	if err := json.Unmarshal(detail, &detailStr); err == nil {
		_ = json.Unmarshal([]byte(detailStr), &d)
	} else {
		_ = json.Unmarshal(detail, &d)
	}
	return d.Domain
}
