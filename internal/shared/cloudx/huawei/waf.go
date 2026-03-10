package huawei

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	wafv1 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/waf/v1"
	wafmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/waf/v1/model"
	wafregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/waf/v1/region"
)

// WAFAdapter 华为云WAF适配器
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
func (a *WAFAdapter) createClient(reg string) (*wafv1.WafClient, error) {
	if reg == "" {
		reg = a.defaultRegion
	}

	auth, err := basic.NewCredentialsBuilder().
		WithAk(a.accessKeyID).
		WithSk(a.accessKeySecret).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建华为云凭证失败: %w", err)
	}

	r, err := wafregion.SafeValueOf(reg)
	if err != nil {
		return nil, fmt.Errorf("无效的地域: %s", reg)
	}

	client, err := wafv1.WafClientBuilder().
		WithRegion(r).
		WithCredential(auth).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建华为云WAF客户端失败: %w", err)
	}

	return wafv1.NewWafClient(client), nil
}

// ListInstances 获取WAF实例列表
func (a *WAFAdapter) ListInstances(ctx context.Context, region string) ([]types.WAFInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, nil)
}

// GetInstance 获取单个WAF实例详情
func (a *WAFAdapter) GetInstance(ctx context.Context, region, instanceID string) (*types.WAFInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := &wafmodel.ShowInstanceRequest{
		InstanceId: instanceID,
	}

	response, err := client.ShowInstance(request)
	if err != nil {
		return nil, fmt.Errorf("获取WAF实例详情失败: %w", err)
	}

	instance := a.convertDetailToInstance(response, region)
	return &instance, nil
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
	page := int32(1)
	pageSize := int32(100)

	if filter != nil && filter.PageSize > 0 {
		pageSize = int32(filter.PageSize)
	}

	for {
		request := &wafmodel.ListInstanceRequest{
			Page:     &page,
			Pagesize: &pageSize,
		}

		if filter != nil && filter.InstanceName != "" {
			request.Instancename = &filter.InstanceName
		}

		response, err := client.ListInstance(request)
		if err != nil {
			return nil, fmt.Errorf("获取WAF实例列表失败: %w", err)
		}

		if response.Items == nil || len(*response.Items) == 0 {
			break
		}

		for _, item := range *response.Items {
			allInstances = append(allInstances, a.convertToInstance(item, region))
		}

		if len(*response.Items) < int(pageSize) {
			break
		}
		page++
	}

	a.logger.Info("获取华为云WAF实例列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))
	return allInstances, nil
}

// convertToInstance 转换列表项为通用WAF实例
func (a *WAFAdapter) convertToInstance(item wafmodel.ListInstance, region string) types.WAFInstance {
	instanceID := ""
	if item.Id != nil {
		instanceID = *item.Id
	}
	instanceName := ""
	if item.Instancename != nil {
		instanceName = *item.Instancename
	}

	return types.WAFInstance{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		Status:       "active",
		Region:       region,
		Provider:     "huawei",
		Tags:         make(map[string]string),
	}
}

// convertDetailToInstance 转换详情为通用WAF实例
func (a *WAFAdapter) convertDetailToInstance(resp *wafmodel.ShowInstanceResponse, region string) types.WAFInstance {
	instanceID := ""
	if resp.Id != nil {
		instanceID = *resp.Id
	}
	instanceName := ""
	if resp.Instancename != nil {
		instanceName = *resp.Instancename
	}

	return types.WAFInstance{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		Status:       "active",
		Region:       region,
		Provider:     "huawei",
		Tags:         make(map[string]string),
	}
}
