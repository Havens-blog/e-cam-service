package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
	"github.com/gotomicro/ego/core/elog"
)

// LBAdapter 阿里云负载均衡适配器
type LBAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewLBAdapter 创建负载均衡适配器
func NewLBAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *LBAdapter {
	return &LBAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建SLB客户端
func (a *LBAdapter) createClient(region string) (*slb.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	return slb.NewClientWithAccessKey(region, a.accessKeyID, a.accessKeySecret)
}

// ListInstances 获取负载均衡实例列表
func (a *LBAdapter) ListInstances(ctx context.Context, region string) ([]types.LBInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个负载均衡实例详情
func (a *LBAdapter) GetInstance(ctx context.Context, region, lbID string) (*types.LBInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建SLB客户端失败: %w", err)
	}

	request := slb.CreateDescribeLoadBalancerAttributeRequest()
	request.RegionId = region
	request.LoadBalancerId = lbID

	response, err := client.DescribeLoadBalancerAttribute(request)
	if err != nil {
		return nil, fmt.Errorf("获取负载均衡详情失败: %w", err)
	}

	instance := a.convertDetailToLBInstance(response, region)
	return &instance, nil
}

// ListInstancesByIDs 批量获取负载均衡实例
func (a *LBAdapter) ListInstancesByIDs(ctx context.Context, region string, lbIDs []string) ([]types.LBInstance, error) {
	var result []types.LBInstance
	for _, lbID := range lbIDs {
		instance, err := a.GetInstance(ctx, region, lbID)
		if err != nil {
			a.logger.Warn("获取负载均衡失败", elog.String("lb_id", lbID), elog.FieldErr(err))
			continue
		}
		result = append(result, *instance)
	}
	return result, nil
}

// GetInstanceStatus 获取实例状态
func (a *LBAdapter) GetInstanceStatus(ctx context.Context, region, lbID string) (string, error) {
	instance, err := a.GetInstance(ctx, region, lbID)
	if err != nil {
		return "", err
	}
	return instance.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取实例列表
func (a *LBAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.LBInstanceFilter) ([]types.LBInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建SLB客户端失败: %w", err)
	}

	var allInstances []types.LBInstance
	pageNumber := 1
	pageSize := 50

	if filter != nil {
		if filter.PageSize > 0 {
			pageSize = filter.PageSize
		}
	}

	for {
		request := slb.CreateDescribeLoadBalancersRequest()
		request.RegionId = region
		request.PageNumber = requests.NewInteger(pageNumber)
		request.PageSize = requests.NewInteger(pageSize)

		if filter != nil {
			if filter.LoadBalancerName != "" {
				request.LoadBalancerName = filter.LoadBalancerName
			}
			if filter.AddressType != "" {
				request.AddressType = filter.AddressType
			}
			if filter.VPCID != "" {
				request.VpcId = filter.VPCID
			}
			if len(filter.LoadBalancerIDs) == 1 {
				request.LoadBalancerId = filter.LoadBalancerIDs[0]
			}
		}

		response, err := client.DescribeLoadBalancers(request)
		if err != nil {
			return nil, fmt.Errorf("获取负载均衡列表失败: %w", err)
		}

		for _, lb := range response.LoadBalancers.LoadBalancer {
			allInstances = append(allInstances, a.convertToLBInstance(lb, region))
		}

		if len(response.LoadBalancers.LoadBalancer) < pageSize {
			break
		}
		pageNumber++
	}

	// 查询 ALB 实例
	albInstances, err := a.listALBInstances(region)
	if err != nil {
		a.logger.Warn("获取ALB实例列表失败", elog.String("region", region), elog.FieldErr(err))
	} else {
		allInstances = append(allInstances, albInstances...)
	}

	// 查询 NLB 实例
	nlbInstances, err := a.listNLBInstances(region)
	if err != nil {
		a.logger.Warn("获取NLB实例列表失败", elog.String("region", region), elog.FieldErr(err))
	} else {
		allInstances = append(allInstances, nlbInstances...)
	}

	a.logger.Info("获取阿里云负载均衡列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))

	return allInstances, nil
}

// convertToLBInstance 转换为通用LB实例
func (a *LBAdapter) convertToLBInstance(lb slb.LoadBalancer, region string) types.LBInstance {
	tags := make(map[string]string)

	// 判断LB类型
	lbType := "slb"
	if lb.LoadBalancerSpec != "" {
		lbType = "slb"
	}

	return types.LBInstance{
		LoadBalancerID:     lb.LoadBalancerId,
		LoadBalancerName:   lb.LoadBalancerName,
		LoadBalancerType:   lbType,
		Status:             lb.LoadBalancerStatus,
		Region:             region,
		Zone:               lb.MasterZoneId,
		SlaveZone:          lb.SlaveZoneId,
		Address:            lb.Address,
		AddressType:        lb.AddressType,
		AddressIPVersion:   lb.AddressIPVersion,
		VPCID:              lb.VpcId,
		VSwitchID:          lb.VSwitchId,
		NetworkType:        lb.NetworkType,
		LoadBalancerSpec:   lb.LoadBalancerSpec,
		Bandwidth:          lb.Bandwidth,
		InternetChargeType: lb.InternetChargeType,
		ChargeType:         lb.PayType,
		CreationTime:       lb.CreateTime,
		ResourceGroupID:    lb.ResourceGroupId,
		Tags:               tags,
		Provider:           "aliyun",
	}
}

// convertDetailToLBInstance 从详情接口转换为通用LB实例
func (a *LBAdapter) convertDetailToLBInstance(resp *slb.DescribeLoadBalancerAttributeResponse, region string) types.LBInstance {
	tags := make(map[string]string)

	lbType := "slb"

	// 提取监听器详情
	var listeners []types.LBListener
	for _, l := range resp.ListenerPortsAndProtocol.ListenerPortAndProtocol {
		listeners = append(listeners, types.LBListener{
			ListenerPort:     l.ListenerPort,
			ListenerProtocol: l.ListenerProtocol,
			Description:      l.ListenerForward,
		})
	}

	// 提取后端服务器详情
	var backendServers []types.LBBackendServer
	for _, bs := range resp.BackendServers.BackendServer {
		backendServers = append(backendServers, types.LBBackendServer{
			ServerID:    bs.ServerId,
			Weight:      bs.Weight,
			Type:        bs.Type,
			Description: bs.Description,
		})
	}

	return types.LBInstance{
		LoadBalancerID:     resp.LoadBalancerId,
		LoadBalancerName:   resp.LoadBalancerName,
		LoadBalancerType:   lbType,
		Status:             resp.LoadBalancerStatus,
		Region:             region,
		Zone:               resp.MasterZoneId,
		SlaveZone:          resp.SlaveZoneId,
		Address:            resp.Address,
		AddressType:        resp.AddressType,
		AddressIPVersion:   resp.AddressIPVersion,
		VPCID:              resp.VpcId,
		VSwitchID:          resp.VSwitchId,
		NetworkType:        resp.NetworkType,
		LoadBalancerSpec:   resp.LoadBalancerSpec,
		Bandwidth:          resp.Bandwidth,
		InternetChargeType: resp.InternetChargeType,
		ChargeType:         resp.PayType,
		CreationTime:       resp.CreateTime,
		ResourceGroupID:    resp.ResourceGroupId,
		ListenerCount:      len(resp.ListenerPortsAndProtocol.ListenerPortAndProtocol),
		BackendServerCount: len(resp.BackendServers.BackendServer),
		Listeners:          listeners,
		BackendServers:     backendServers,
		Tags:               tags,
		Description:        resp.LoadBalancerName,
		Provider:           "aliyun",
	}
}

// ==================== ALB (Application Load Balancer) ====================

// albListResponse ALB ListLoadBalancers 响应
type albListResponse struct {
	RequestID     string        `json:"RequestId"`
	LoadBalancers []albInstance `json:"LoadBalancers"`
	TotalCount    int           `json:"TotalCount"`
	NextToken     string        `json:"NextToken"`
}

// albInstance ALB 实例
type albInstance struct {
	LoadBalancerID       string `json:"LoadBalancerId"`
	LoadBalancerName     string `json:"LoadBalancerName"`
	LoadBalancerStatus   string `json:"LoadBalancerStatus"`
	AddressType          string `json:"AddressType"`
	AddressAllocatedMode string `json:"AddressAllocatedMode"`
	VpcID                string `json:"VpcId"`
	CreateTime           string `json:"CreateTime"`
	LoadBalancerEdition  string `json:"LoadBalancerEdition"`
	DNSName              string `json:"DNSName"`
	ResourceGroupID      string `json:"ResourceGroupId"`
	AddressIPVersion     string `json:"AddressIpVersion"`
}

// albListListenersResponse ALB ListListeners 响应
type albListListenersResponse struct {
	RequestID  string        `json:"RequestId"`
	Listeners  []albListener `json:"Listeners"`
	TotalCount int           `json:"TotalCount"`
	NextToken  string        `json:"NextToken"`
}

// albListener ALB 监听器
type albListener struct {
	ListenerID       string `json:"ListenerId"`
	ListenerPort     int    `json:"ListenerPort"`
	ListenerProtocol string `json:"ListenerProtocol"`
	ListenerStatus   string `json:"ListenerStatus"`
}

// albListServerGroupsResponse ALB ListServerGroups 响应
type albListServerGroupsResponse struct {
	RequestID    string           `json:"RequestId"`
	ServerGroups []albServerGroup `json:"ServerGroups"`
	TotalCount   int              `json:"TotalCount"`
	NextToken    string           `json:"NextToken"`
}

// albServerGroup ALB 服务器组
type albServerGroup struct {
	ServerGroupID   string `json:"ServerGroupId"`
	ServerGroupName string `json:"ServerGroupName"`
	ServerCount     int    `json:"ServerCount"`
}

// nlbListListenersResponse NLB ListListeners 响应
type nlbListListenersResponse struct {
	RequestID  string        `json:"RequestId"`
	Listeners  []nlbListener `json:"Listeners"`
	TotalCount int           `json:"TotalCount"`
	NextToken  string        `json:"NextToken"`
}

// nlbListener NLB 监听器
type nlbListener struct {
	ListenerID       string `json:"ListenerId"`
	ListenerPort     int    `json:"ListenerPort"`
	ListenerProtocol string `json:"ListenerProtocol"`
	ListenerStatus   string `json:"ListenerStatus"`
}

// nlbListServerGroupsResponse NLB ListServerGroups 响应
type nlbListServerGroupsResponse struct {
	RequestID    string           `json:"RequestId"`
	ServerGroups []nlbServerGroup `json:"ServerGroups"`
	TotalCount   int              `json:"TotalCount"`
	NextToken    string           `json:"NextToken"`
}

// nlbServerGroup NLB 服务器组
type nlbServerGroup struct {
	ServerGroupID   string `json:"ServerGroupId"`
	ServerGroupName string `json:"ServerGroupName"`
	ServerCount     int    `json:"ServerCount"`
}

// createCommonClient 创建通用SDK客户端
func (a *LBAdapter) createCommonClient(region string) (*sdk.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	return sdk.NewClientWithAccessKey(region, a.accessKeyID, a.accessKeySecret)
}

// listALBInstances 获取ALB实例列表
func (a *LBAdapter) listALBInstances(region string) ([]types.LBInstance, error) {
	client, err := a.createCommonClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建ALB客户端失败: %w", err)
	}

	var allInstances []types.LBInstance
	nextToken := ""
	pageSize := 50

	for {
		request := requests.NewCommonRequest()
		request.Method = "POST"
		request.Scheme = "https"
		request.Domain = fmt.Sprintf("alb.%s.aliyuncs.com", region)
		request.Version = "2020-06-16"
		request.ApiName = "ListLoadBalancers"
		request.QueryParams["RegionId"] = region
		request.QueryParams["MaxResults"] = strconv.Itoa(pageSize)

		if nextToken != "" {
			request.QueryParams["NextToken"] = nextToken
		}

		response, err := client.ProcessCommonRequest(request)
		if err != nil {
			a.logger.Warn("获取ALB实例列表失败", elog.String("region", region), elog.FieldErr(err))
			return nil, nil
		}

		rawBody := string(response.GetHttpContentBytes())
		var resp albListResponse
		if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
			truncated := rawBody
			if len(truncated) > 500 {
				truncated = truncated[:500]
			}
			a.logger.Warn("解析ALB响应失败", elog.String("region", region), elog.String("raw", truncated))
			return nil, fmt.Errorf("解析ALB响应失败: %w", err)
		}

		a.logger.Info("ALB查询结果", elog.String("region", region), elog.Int("count", len(resp.LoadBalancers)), elog.Int("total", resp.TotalCount))

		for _, lb := range resp.LoadBalancers {
			allInstances = append(allInstances, a.convertALBToLBInstance(lb, region))
		}

		if resp.NextToken == "" || len(resp.LoadBalancers) == 0 {
			break
		}
		nextToken = resp.NextToken
	}

	// 为每个ALB实例获取监听器和后端服务器组信息
	for i := range allInstances {
		listeners, listenerCount := a.fetchALBListeners(client, region, allInstances[i].LoadBalancerID)
		allInstances[i].Listeners = listeners
		allInstances[i].ListenerCount = listenerCount

		backendServerCount := a.fetchALBServerGroupCount(client, region, allInstances[i].LoadBalancerID)
		allInstances[i].BackendServerCount = backendServerCount
	}

	return allInstances, nil
}

// fetchALBListeners 获取ALB监听器列表
func (a *LBAdapter) fetchALBListeners(client *sdk.Client, region, lbID string) ([]types.LBListener, int) {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = fmt.Sprintf("alb.%s.aliyuncs.com", region)
	request.Version = "2020-06-16"
	request.ApiName = "ListListeners"
	request.QueryParams["LoadBalancerIds.1"] = lbID

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		a.logger.Warn("获取ALB监听器列表失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return nil, 0
	}

	var resp albListListenersResponse
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		a.logger.Warn("解析ALB监听器响应失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return nil, 0
	}

	var listeners []types.LBListener
	for _, l := range resp.Listeners {
		listeners = append(listeners, types.LBListener{
			ListenerID:       l.ListenerID,
			ListenerPort:     l.ListenerPort,
			ListenerProtocol: l.ListenerProtocol,
			Status:           l.ListenerStatus,
		})
	}

	return listeners, resp.TotalCount
}

// fetchALBServerGroupCount 获取ALB服务器组数量作为后端服务器计数
func (a *LBAdapter) fetchALBServerGroupCount(client *sdk.Client, region, lbID string) int {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = fmt.Sprintf("alb.%s.aliyuncs.com", region)
	request.Version = "2020-06-16"
	request.ApiName = "ListServerGroups"
	request.QueryParams["LoadBalancerId"] = lbID // 注意：此参数可能不被直接支持，仅获取总数

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		a.logger.Warn("获取ALB服务器组列表失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return 0
	}

	var resp albListServerGroupsResponse
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		a.logger.Warn("解析ALB服务器组响应失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return 0
	}

	// 累加所有服务器组中的服务器数量
	totalServers := 0
	for _, sg := range resp.ServerGroups {
		totalServers += sg.ServerCount
	}

	return totalServers
}

// convertALBToLBInstance 将ALB实例转换为通用LB实例
func (a *LBAdapter) convertALBToLBInstance(lb albInstance, region string) types.LBInstance {
	address := lb.DNSName

	return types.LBInstance{
		LoadBalancerID:      lb.LoadBalancerID,
		LoadBalancerName:    lb.LoadBalancerName,
		LoadBalancerType:    "alb",
		Status:              lb.LoadBalancerStatus,
		Region:              region,
		Address:             address,
		AddressType:         lb.AddressType,
		AddressIPVersion:    lb.AddressIPVersion,
		VPCID:               lb.VpcID,
		LoadBalancerEdition: lb.LoadBalancerEdition,
		CreationTime:        lb.CreateTime,
		ResourceGroupID:     lb.ResourceGroupID,
		Tags:                make(map[string]string),
		Provider:            "aliyun",
	}
}

// ==================== NLB (Network Load Balancer) ====================

// nlbListResponse NLB ListLoadBalancers 响应
type nlbListResponse struct {
	RequestID     string        `json:"RequestId"`
	LoadBalancers []nlbInstance `json:"LoadBalancers"`
	TotalCount    int           `json:"TotalCount"`
	NextToken     string        `json:"NextToken"`
}

// nlbInstance NLB 实例
type nlbInstance struct {
	LoadBalancerID     string `json:"LoadBalancerId"`
	LoadBalancerName   string `json:"LoadBalancerName"`
	LoadBalancerStatus string `json:"LoadBalancerStatus"`
	AddressType        string `json:"AddressType"`
	VpcID              string `json:"VpcId"`
	CreateTime         string `json:"CreateTime"`
	DNSName            string `json:"DNSName"`
	ResourceGroupID    string `json:"ResourceGroupId"`
	AddressIPVersion   string `json:"AddressIpVersion"`
	BandwidthPackageID string `json:"BandwidthPackageId"`
}

// listNLBInstances 获取NLB实例列表
func (a *LBAdapter) listNLBInstances(region string) ([]types.LBInstance, error) {
	client, err := a.createCommonClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建NLB客户端失败: %w", err)
	}

	var allInstances []types.LBInstance
	nextToken := ""
	pageSize := 50

	for {
		request := requests.NewCommonRequest()
		request.Method = "POST"
		request.Scheme = "https"
		request.Domain = fmt.Sprintf("nlb.%s.aliyuncs.com", region)
		request.Version = "2022-04-30"
		request.ApiName = "ListLoadBalancers"
		request.QueryParams["RegionId"] = region
		request.QueryParams["MaxResults"] = strconv.Itoa(pageSize)

		if nextToken != "" {
			request.QueryParams["NextToken"] = nextToken
		}

		response, err := client.ProcessCommonRequest(request)
		if err != nil {
			a.logger.Warn("获取NLB实例列表失败", elog.String("region", region), elog.FieldErr(err))
			return nil, nil
		}

		var resp nlbListResponse
		if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
			return nil, fmt.Errorf("解析NLB响应失败: %w", err)
		}

		for _, lb := range resp.LoadBalancers {
			allInstances = append(allInstances, a.convertNLBToLBInstance(lb, region))
		}

		if resp.NextToken == "" || len(resp.LoadBalancers) == 0 {
			break
		}
		nextToken = resp.NextToken
	}

	// 为每个NLB实例获取监听器和后端服务器组信息
	for i := range allInstances {
		listeners, listenerCount := a.fetchNLBListeners(client, region, allInstances[i].LoadBalancerID)
		allInstances[i].Listeners = listeners
		allInstances[i].ListenerCount = listenerCount

		backendServerCount := a.fetchNLBServerGroupCount(client, region, allInstances[i].LoadBalancerID)
		allInstances[i].BackendServerCount = backendServerCount
	}

	return allInstances, nil
}

// fetchNLBListeners 获取NLB监听器列表
func (a *LBAdapter) fetchNLBListeners(client *sdk.Client, region, lbID string) ([]types.LBListener, int) {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = fmt.Sprintf("nlb.%s.aliyuncs.com", region)
	request.Version = "2022-04-30"
	request.ApiName = "ListListeners"
	request.QueryParams["LoadBalancerIds.1"] = lbID

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		a.logger.Warn("获取NLB监听器列表失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return nil, 0
	}

	var resp nlbListListenersResponse
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		a.logger.Warn("解析NLB监听器响应失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return nil, 0
	}

	var listeners []types.LBListener
	for _, l := range resp.Listeners {
		listeners = append(listeners, types.LBListener{
			ListenerID:       l.ListenerID,
			ListenerPort:     l.ListenerPort,
			ListenerProtocol: l.ListenerProtocol,
			Status:           l.ListenerStatus,
		})
	}

	return listeners, resp.TotalCount
}

// fetchNLBServerGroupCount 获取NLB服务器组数量作为后端服务器计数
func (a *LBAdapter) fetchNLBServerGroupCount(client *sdk.Client, region, lbID string) int {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = fmt.Sprintf("nlb.%s.aliyuncs.com", region)
	request.Version = "2022-04-30"
	request.ApiName = "ListServerGroups"
	request.QueryParams["LoadBalancerIds.1"] = lbID

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		a.logger.Warn("获取NLB服务器组列表失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return 0
	}

	var resp nlbListServerGroupsResponse
	if err := json.Unmarshal(response.GetHttpContentBytes(), &resp); err != nil {
		a.logger.Warn("解析NLB服务器组响应失败", elog.String("lb_id", lbID), elog.FieldErr(err))
		return 0
	}

	// 累加所有服务器组中的服务器数量
	totalServers := 0
	for _, sg := range resp.ServerGroups {
		totalServers += sg.ServerCount
	}

	return totalServers
}

// convertNLBToLBInstance 将NLB实例转换为通用LB实例
func (a *LBAdapter) convertNLBToLBInstance(lb nlbInstance, region string) types.LBInstance {
	address := lb.DNSName

	return types.LBInstance{
		LoadBalancerID:     lb.LoadBalancerID,
		LoadBalancerName:   lb.LoadBalancerName,
		LoadBalancerType:   "nlb",
		Status:             lb.LoadBalancerStatus,
		Region:             region,
		Address:            address,
		AddressType:        lb.AddressType,
		AddressIPVersion:   lb.AddressIPVersion,
		VPCID:              lb.VpcID,
		BandwidthPackageID: lb.BandwidthPackageID,
		CreationTime:       lb.CreateTime,
		ResourceGroupID:    lb.ResourceGroupID,
		Tags:               make(map[string]string),
		Provider:           "aliyun",
	}
}
