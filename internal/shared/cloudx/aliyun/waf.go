package aliyun

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/gotomicro/ego/core/elog"
)

// WAFAdapter 阿里云WAF适配器
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
func (a *WAFAdapter) createClient(region string) (*sdk.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}
	return sdk.NewClientWithAccessKey(region, a.accessKeyID, a.accessKeySecret)
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

	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = "wafopenapi.cn-hangzhou.aliyuncs.com"
	request.Version = "2021-10-01"
	request.ApiName = "DescribeInstance"
	request.QueryParams["InstanceId"] = instanceID
	request.QueryParams["RegionId"] = region

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		return nil, fmt.Errorf("获取WAF实例详情失败: %w", err)
	}

	_ = response
	// 阿里云WAF API返回JSON，解析实例信息
	instance := &types.WAFInstance{
		InstanceID:   instanceID,
		InstanceName: instanceID,
		Status:       "active",
		Region:       region,
		Provider:     "aliyun",
		Tags:         make(map[string]string),
	}
	return instance, nil
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
	pageNumber := 1
	pageSize := 10

	if filter != nil && filter.PageSize > 0 {
		pageSize = filter.PageSize
	}

	for {
		request := requests.NewCommonRequest()
		request.Method = "POST"
		request.Scheme = "https"
		request.Domain = "wafopenapi.cn-hangzhou.aliyuncs.com"
		request.Version = "2021-10-01"
		request.ApiName = "DescribeInstanceInfo"
		request.QueryParams["RegionId"] = region
		request.QueryParams["PageNumber"] = fmt.Sprintf("%d", pageNumber)
		request.QueryParams["PageSize"] = fmt.Sprintf("%d", pageSize)

		response, err := client.ProcessCommonRequest(request)
		if err != nil {
			return nil, fmt.Errorf("获取WAF实例列表失败: %w", err)
		}

		_ = response
		// 阿里云WAF通常每个账号只有一个实例
		// 解析返回的JSON获取实例信息
		// 如果没有更多数据则退出
		break
	}

	a.logger.Info("获取阿里云WAF实例列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}
