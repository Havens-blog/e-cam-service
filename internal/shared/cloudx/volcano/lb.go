package volcano

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/volcengine/volcengine-go-sdk/service/clb"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

// LBAdapter 火山引擎负载均衡适配器 (CLB)
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
func (a *LBAdapter) createClient(region string) (*clb.CLB, error) {
	if region == "" {
		region = a.defaultRegion
	}

	config := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(a.accessKeyID, a.accessKeySecret, "")).
		WithRegion(region)

	sess, err := session.NewSession(config)
	if err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	return clb.New(sess), nil
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

	input := &clb.DescribeLoadBalancerAttributesInput{
		LoadBalancerId: volcengine.String(lbID),
	}

	output, err := client.DescribeLoadBalancerAttributes(input)
	if err != nil {
		return nil, fmt.Errorf("获取负载均衡详情失败: %w", err)
	}

	instance := a.convertDetailToLBInstance(output, region)
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

	idPtrs := make([]*string, len(lbIDs))
	for i, id := range lbIDs {
		idPtrs[i] = volcengine.String(id)
	}

	input := &clb.DescribeLoadBalancersInput{
		LoadBalancerIds: idPtrs,
	}

	output, err := client.DescribeLoadBalancers(input)
	if err != nil {
		return nil, fmt.Errorf("批量获取负载均衡失败: %w", err)
	}

	var result []types.LBInstance
	if output.LoadBalancers != nil {
		for _, lb := range output.LoadBalancers {
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
	pageNumber := int64(1)
	pageSize := int64(100)

	if filter != nil && filter.PageSize > 0 {
		pageSize = int64(filter.PageSize)
	}

	for {
		input := &clb.DescribeLoadBalancersInput{
			PageNumber: &pageNumber,
			PageSize:   &pageSize,
		}

		if filter != nil {
			if len(filter.LoadBalancerIDs) > 0 {
				idPtrs := make([]*string, len(filter.LoadBalancerIDs))
				for i, id := range filter.LoadBalancerIDs {
					idPtrs[i] = volcengine.String(id)
				}
				input.LoadBalancerIds = idPtrs
			}
			if filter.LoadBalancerName != "" {
				input.LoadBalancerName = volcengine.String(filter.LoadBalancerName)
			}
			if filter.VPCID != "" {
				input.VpcId = volcengine.String(filter.VPCID)
			}
		}

		output, err := client.DescribeLoadBalancers(input)
		if err != nil {
			return nil, fmt.Errorf("获取负载均衡列表失败: %w", err)
		}

		if output.LoadBalancers == nil || len(output.LoadBalancers) == 0 {
			break
		}

		for _, lb := range output.LoadBalancers {
			instance := a.convertToLBInstance(lb, region)

			// 客户端过滤: AddressType
			if filter != nil && filter.AddressType != "" {
				if filter.AddressType != instance.AddressType {
					continue
				}
			}

			allInstances = append(allInstances, instance)
		}

		if len(output.LoadBalancers) < int(pageSize) {
			break
		}
		pageNumber++
	}

	a.logger.Info("获取火山引擎负载均衡列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))

	return allInstances, nil
}

// convertToLBInstance 转换为通用LB实例
func (a *LBAdapter) convertToLBInstance(lb *clb.LoadBalancerForDescribeLoadBalancersOutput, region string) types.LBInstance {
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
		status = *lb.Status
	}

	address := ""
	if lb.EniAddress != nil {
		address = *lb.EniAddress
	}

	addressType := "intranet"
	if lb.Type != nil && *lb.Type == "public" {
		addressType = "internet"
	}

	vpcID := ""
	if lb.VpcId != nil {
		vpcID = *lb.VpcId
	}

	subnetID := ""
	if lb.SubnetId != nil {
		subnetID = *lb.SubnetId
	}

	createTime := ""
	if lb.CreateTime != nil {
		createTime = *lb.CreateTime
	}

	description := ""
	if lb.Description != nil {
		description = *lb.Description
	}

	// 提取标签
	tags := make(map[string]string)
	if lb.Tags != nil {
		for _, tag := range lb.Tags {
			if tag.Key != nil && tag.Value != nil {
				tags[*tag.Key] = *tag.Value
			}
		}
	}

	return types.LBInstance{
		LoadBalancerID:   lbID,
		LoadBalancerName: lbName,
		LoadBalancerType: lbType,
		Status:           status,
		Region:           region,
		Address:          address,
		AddressType:      addressType,
		VPCID:            vpcID,
		VSwitchID:        subnetID,
		CreationTime:     createTime,
		Description:      description,
		Tags:             tags,
		Provider:         "volcano",
	}
}

// convertDetailToLBInstance 从详情接口转换为通用LB实例
func (a *LBAdapter) convertDetailToLBInstance(output *clb.DescribeLoadBalancerAttributesOutput, region string) types.LBInstance {
	lbID := ""
	if output.LoadBalancerId != nil {
		lbID = *output.LoadBalancerId
	}

	lbName := ""
	if output.LoadBalancerName != nil {
		lbName = *output.LoadBalancerName
	}

	lbType := "slb"

	status := ""
	if output.Status != nil {
		status = *output.Status
	}

	address := ""
	if output.EniAddress != nil {
		address = *output.EniAddress
	}

	addressType := "intranet"
	if output.Type != nil && *output.Type == "public" {
		addressType = "internet"
	}

	vpcID := ""
	if output.VpcId != nil {
		vpcID = *output.VpcId
	}

	subnetID := ""
	if output.SubnetId != nil {
		subnetID = *output.SubnetId
	}

	createTime := ""
	if output.CreateTime != nil {
		createTime = *output.CreateTime
	}

	description := ""
	if output.Description != nil {
		description = *output.Description
	}

	listenerCount := 0
	if output.Listeners != nil {
		listenerCount = len(output.Listeners)
	}

	// 提取标签
	tags := make(map[string]string)
	if output.Tags != nil {
		for _, tag := range output.Tags {
			if tag.Key != nil && tag.Value != nil {
				tags[*tag.Key] = *tag.Value
			}
		}
	}

	return types.LBInstance{
		LoadBalancerID:   lbID,
		LoadBalancerName: lbName,
		LoadBalancerType: lbType,
		Status:           status,
		Region:           region,
		Address:          address,
		AddressType:      addressType,
		VPCID:            vpcID,
		VSwitchID:        subnetID,
		ListenerCount:    listenerCount,
		CreationTime:     createTime,
		Description:      description,
		Tags:             tags,
		Provider:         "volcano",
	}
}
