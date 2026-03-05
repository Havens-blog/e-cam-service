package aliyun

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
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
		Tags:               tags,
		Description:        resp.LoadBalancerName,
		Provider:           "aliyun",
	}
}
