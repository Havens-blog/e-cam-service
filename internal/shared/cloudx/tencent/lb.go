package tencent

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	clb "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/clb/v20180317"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// LBAdapter 腾讯云负载均衡适配器 (CLB)
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

// createClient 创建CLB客户端
func (a *LBAdapter) createClient(region string) (*clb.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}

	credential := common.NewCredential(a.accessKeyID, a.accessKeySecret)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "clb.tencentcloudapi.com"

	return clb.NewClient(credential, region, cpf)
}

// ListInstances 获取负载均衡实例列表
func (a *LBAdapter) ListInstances(ctx context.Context, region string) ([]types.LBInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个负载均衡实例详情
func (a *LBAdapter) GetInstance(ctx context.Context, region, lbID string) (*types.LBInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := clb.NewDescribeLoadBalancersRequest()
	request.LoadBalancerIds = common.StringPtrs([]string{lbID})

	response, err := client.DescribeLoadBalancers(request)
	if err != nil {
		return nil, fmt.Errorf("获取负载均衡详情失败: %w", err)
	}

	if response.Response.LoadBalancerSet == nil || len(response.Response.LoadBalancerSet) == 0 {
		return nil, fmt.Errorf("负载均衡不存在: %s", lbID)
	}

	instance := a.convertToLBInstance(response.Response.LoadBalancerSet[0], region)
	return &instance, nil
}

// ListInstancesByIDs 批量获取负载均衡实例
func (a *LBAdapter) ListInstancesByIDs(ctx context.Context, region string, lbIDs []string) ([]types.LBInstance, error) {
	if len(lbIDs) == 0 {
		return nil, nil
	}

	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := clb.NewDescribeLoadBalancersRequest()
	request.LoadBalancerIds = common.StringPtrs(lbIDs)

	response, err := client.DescribeLoadBalancers(request)
	if err != nil {
		return nil, fmt.Errorf("批量获取负载均衡失败: %w", err)
	}

	var result []types.LBInstance
	if response.Response.LoadBalancerSet != nil {
		for _, lb := range response.Response.LoadBalancerSet {
			result = append(result, a.convertToLBInstance(lb, region))
		}
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
		return nil, fmt.Errorf("创建CLB客户端失败: %w", err)
	}

	var allInstances []types.LBInstance
	offset := int64(0)
	limit := int64(100)

	if filter != nil && filter.PageSize > 0 {
		limit = int64(filter.PageSize)
	}

	for {
		request := clb.NewDescribeLoadBalancersRequest()
		request.Offset = &offset
		request.Limit = &limit

		if filter != nil {
			if len(filter.LoadBalancerIDs) > 0 {
				request.LoadBalancerIds = common.StringPtrs(filter.LoadBalancerIDs)
			}
			if filter.LoadBalancerName != "" {
				request.LoadBalancerName = &filter.LoadBalancerName
			}
			if filter.VPCID != "" {
				request.VpcId = &filter.VPCID
			}
			if filter.AddressType != "" {
				loadBalancerType := "INTERNAL"
				if filter.AddressType == "internet" {
					loadBalancerType = "OPEN"
				}
				request.LoadBalancerType = &loadBalancerType
			}
		}

		response, err := client.DescribeLoadBalancers(request)
		if err != nil {
			return nil, fmt.Errorf("获取负载均衡列表失败: %w", err)
		}

		if response.Response.LoadBalancerSet == nil || len(response.Response.LoadBalancerSet) == 0 {
			break
		}

		for _, lb := range response.Response.LoadBalancerSet {
			allInstances = append(allInstances, a.convertToLBInstance(lb, region))
		}

		if len(response.Response.LoadBalancerSet) < int(limit) {
			break
		}
		offset += limit
	}

	a.logger.Info("获取腾讯云负载均衡列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))

	return allInstances, nil
}

// convertToLBInstance 转换为通用LB实例
func (a *LBAdapter) convertToLBInstance(lb *clb.LoadBalancer, region string) types.LBInstance {
	lbID := ""
	if lb.LoadBalancerId != nil {
		lbID = *lb.LoadBalancerId
	}

	lbName := ""
	if lb.LoadBalancerName != nil {
		lbName = *lb.LoadBalancerName
	}

	lbType := "slb"

	status := ""
	if lb.Status != nil {
		switch *lb.Status {
		case 0:
			status = "Creating"
		case 1:
			status = "Active"
		default:
			status = fmt.Sprintf("%d", *lb.Status)
		}
	}

	addressType := "intranet"
	if lb.LoadBalancerType != nil && *lb.LoadBalancerType == "OPEN" {
		addressType = "internet"
	}

	address := ""
	if lb.LoadBalancerVips != nil && len(lb.LoadBalancerVips) > 0 {
		address = *lb.LoadBalancerVips[0]
	}

	vpcID := ""
	if lb.VpcId != nil {
		vpcID = *lb.VpcId
	}

	subnetID := ""
	if lb.SubnetId != nil {
		subnetID = *lb.SubnetId
	}

	zone := ""
	if lb.MasterZone != nil && lb.MasterZone.Zone != nil {
		zone = *lb.MasterZone.Zone
	}

	slaveZone := ""
	if lb.Zones != nil && len(lb.Zones) > 1 && lb.Zones[1] != nil {
		slaveZone = *lb.Zones[1]
	}

	createTime := ""
	if lb.CreateTime != nil {
		createTime = *lb.CreateTime
	}

	expiredTime := ""
	if lb.ExpireTime != nil {
		expiredTime = *lb.ExpireTime
	}

	chargeType := ""
	if lb.ChargeType != nil {
		chargeType = *lb.ChargeType
	}

	projectID := ""
	if lb.ProjectId != nil {
		projectID = fmt.Sprintf("%d", *lb.ProjectId)
	}

	tags := make(map[string]string)
	if lb.Tags != nil {
		for _, tag := range lb.Tags {
			if tag.TagKey != nil && tag.TagValue != nil {
				tags[*tag.TagKey] = *tag.TagValue
			}
		}
	}

	addressIPVersion := "ipv4"
	if lb.AddressIPVersion != nil {
		addressIPVersion = *lb.AddressIPVersion
	}

	return types.LBInstance{
		LoadBalancerID:   lbID,
		LoadBalancerName: lbName,
		LoadBalancerType: lbType,
		Status:           status,
		Region:           region,
		Zone:             zone,
		SlaveZone:        slaveZone,
		Address:          address,
		AddressType:      addressType,
		AddressIPVersion: addressIPVersion,
		VPCID:            vpcID,
		VSwitchID:        subnetID,
		ChargeType:       chargeType,
		CreationTime:     createTime,
		ExpiredTime:      expiredTime,
		ProjectID:        projectID,
		Tags:             tags,
		Provider:         "tencent",
	}
}
