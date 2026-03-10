package tencent

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	waf "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/waf/v20180125"
)

// WAFAdapter 腾讯云WAF适配器
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

// createClient 创建WAF客户端
func (a *WAFAdapter) createClient(region string) (*waf.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	credential := common.NewCredential(a.accessKeyID, a.accessKeySecret)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "waf.tencentcloudapi.com"
	return waf.NewClient(credential, region, cpf)
}

// ListInstances 获取WAF实例列表
func (a *WAFAdapter) ListInstances(ctx context.Context, region string) ([]types.WAFInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个WAF实例详情
func (a *WAFAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.WAFInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建WAF客户端失败: %w", err)
	}

	request := waf.NewDescribeInstancesRequest()
	request.Filters = []*waf.FiltersItemNew{
		{
			Name:       common.StringPtr("InstanceId"),
			Values:     common.StringPtrs([]string{instanceID}),
			ExactMatch: common.BoolPtr(true),
		},
	}

	response, err := client.DescribeInstances(request)
	if err != nil {
		return nil, fmt.Errorf("获取WAF实例详情失败: %w", err)
	}

	if response.Response.Instances == nil || len(response.Response.Instances) == 0 {
		return nil, fmt.Errorf("WAF实例不存在: %s", instanceID)
	}

	inst := a.convertToInstance(response.Response.Instances[0], region)
	return &inst, nil
}

// ListInstancesByIDs 批量获取WAF实例
func (a *WAFAdapter) ListInstancesByIDs(ctx context.Context, region string, instanceIDs []string) ([]types.WAFInstance, error) {
	var result []types.WAFInstance
	for _, id := range instanceIDs {
		inst, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取WAF实例失败", elog.String("instance_id", id), elog.FieldErr(err))
			continue
		}
		result = append(result, *inst)
	}
	return result, nil
}

// GetInstanceStatus 获取实例状态
func (a *WAFAdapter) GetInstanceStatus(ctx context.Context, region, instanceID string) (string, error) {
	inst, err := a.GetInstance(ctx, region, instanceID)
	if err != nil {
		return "", err
	}
	return inst.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取实例列表
func (a *WAFAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.WAFInstanceFilter) ([]types.WAFInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建WAF客户端失败: %w", err)
	}

	var allInstances []types.WAFInstance
	offset := uint64(0)
	limit := uint64(100)

	if filter != nil && filter.PageSize > 0 {
		limit = uint64(filter.PageSize)
	}

	for {
		request := waf.NewDescribeInstancesRequest()
		request.Offset = &offset
		request.Limit = &limit

		if filter != nil && filter.InstanceName != "" {
			request.Filters = []*waf.FiltersItemNew{
				{
					Name:   common.StringPtr("InstanceName"),
					Values: common.StringPtrs([]string{filter.InstanceName}),
				},
			}
		}

		response, err := client.DescribeInstances(request)
		if err != nil {
			return nil, fmt.Errorf("获取WAF实例列表失败: %w", err)
		}

		if response.Response.Instances == nil || len(response.Response.Instances) == 0 {
			break
		}

		for _, inst := range response.Response.Instances {
			allInstances = append(allInstances, a.convertToInstance(inst, region))
		}

		if len(response.Response.Instances) < int(limit) {
			break
		}
		offset += limit
	}

	a.logger.Info("获取腾讯云WAF实例列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}

// convertToInstance 转换为通用WAF实例
func (a *WAFAdapter) convertToInstance(inst *waf.InstanceInfo, region string) types.WAFInstance {
	instanceID := ""
	if inst.InstanceId != nil {
		instanceID = *inst.InstanceId
	}
	instanceName := ""
	if inst.InstanceName != nil {
		instanceName = *inst.InstanceName
	}
	edition := ""
	if inst.Edition != nil {
		edition = *inst.Edition
	}

	status := "active"

	return types.WAFInstance{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		Status:       status,
		Region:       region,
		Edition:      edition,
		Provider:     "tencent",
		Tags:         make(map[string]string),
	}
}
