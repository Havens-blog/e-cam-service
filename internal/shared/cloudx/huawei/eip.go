package huawei

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	eip "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
)

// EIPAdapter 华为云EIP适配器
type EIPAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewEIPAdapter 创建EIP适配器
func NewEIPAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *EIPAdapter {
	return &EIPAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建EIP客户端
func (a *EIPAdapter) createClient(region string) (*eip.EipClient, error) {
	if region == "" {
		region = a.defaultRegion
	}

	auth, err := basic.NewCredentialsBuilder().
		WithAk(a.accessKeyID).
		WithSk(a.accessKeySecret).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建华为云凭证失败: %w", err)
	}

	r, err := eipregion.SafeValueOf(region)
	if err != nil {
		return nil, fmt.Errorf("无效的地域: %s", region)
	}

	client, err := eip.EipClientBuilder().
		WithRegion(r).
		WithCredential(auth).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建华为云EIP客户端失败: %w", err)
	}

	return eip.NewEipClient(client), nil
}

// ListInstances 获取EIP列表
func (a *EIPAdapter) ListInstances(ctx context.Context, region string) ([]types.EIPInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	var allEIPs []types.EIPInstance
	var marker *string
	limit := int32(100)

	for {
		request := &model.ListPublicipsRequest{
			Limit:  &limit,
			Marker: marker,
		}

		response, err := client.ListPublicips(request)
		if err != nil {
			return nil, fmt.Errorf("获取EIP列表失败: %w", err)
		}

		if response.Publicips == nil {
			break
		}

		for _, e := range *response.Publicips {
			allEIPs = append(allEIPs, a.convertToEIPInstance(e, region))
		}

		if len(*response.Publicips) < int(limit) {
			break
		}
		lastEIP := (*response.Publicips)[len(*response.Publicips)-1]
		marker = lastEIP.Id
	}

	a.logger.Info("获取华为云EIP列表成功",
		elog.String("region", region),
		elog.Int("count", len(allEIPs)))

	return allEIPs, nil
}

// GetInstance 获取单个EIP详情
func (a *EIPAdapter) GetInstance(ctx context.Context, region, allocationID string) (*types.EIPInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := &model.ShowPublicipRequest{
		PublicipId: allocationID,
	}

	response, err := client.ShowPublicip(request)
	if err != nil {
		return nil, fmt.Errorf("获取EIP详情失败: %w", err)
	}

	if response.Publicip == nil {
		return nil, fmt.Errorf("EIP不存在: %s", allocationID)
	}

	instance := a.convertDetailToEIPInstance(*response.Publicip, region)
	return &instance, nil
}

// ListInstancesByIDs 批量获取EIP
func (a *EIPAdapter) ListInstancesByIDs(ctx context.Context, region string, allocationIDs []string) ([]types.EIPInstance, error) {
	var result []types.EIPInstance
	for _, id := range allocationIDs {
		instance, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取EIP失败", elog.String("allocation_id", id), elog.FieldErr(err))
			continue
		}
		result = append(result, *instance)
	}
	return result, nil
}

// GetInstanceStatus 获取EIP状态
func (a *EIPAdapter) GetInstanceStatus(ctx context.Context, region, allocationID string) (string, error) {
	instance, err := a.GetInstance(ctx, region, allocationID)
	if err != nil {
		return "", err
	}
	return instance.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取EIP列表
func (a *EIPAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.EIPInstanceFilter) ([]types.EIPInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := &model.ListPublicipsRequest{}

	if filter != nil {
		if len(filter.IPAddresses) > 0 {
			request.PublicIpAddress = &filter.IPAddresses
		}
		if filter.PageSize > 0 {
			limit := int32(filter.PageSize)
			request.Limit = &limit
		}
	}

	response, err := client.ListPublicips(request)
	if err != nil {
		return nil, fmt.Errorf("获取EIP列表失败: %w", err)
	}

	var result []types.EIPInstance
	if response.Publicips != nil {
		for _, e := range *response.Publicips {
			result = append(result, a.convertToEIPInstance(e, region))
		}
	}

	return result, nil
}

// convertToEIPInstance 转换为通用EIP实例
func (a *EIPAdapter) convertToEIPInstance(e model.PublicipShowResp, region string) types.EIPInstance {
	allocationID := ""
	if e.Id != nil {
		allocationID = *e.Id
	}

	publicIP := ""
	if e.PublicIpAddress != nil {
		publicIP = *e.PublicIpAddress
	}

	status := "Available"
	if e.Status != nil {
		status = e.Status.Value()
	}

	instanceID := ""
	instanceType := ""
	if e.PortId != nil && *e.PortId != "" {
		status = "InUse"
	}

	bandwidth := 0
	if e.BandwidthSize != nil {
		bandwidth = int(*e.BandwidthSize)
	}

	chargeType := ""
	if e.BandwidthShareType != nil {
		chargeType = e.BandwidthShareType.Value()
	}

	createTime := ""
	if e.CreateTime != nil {
		createTime = *e.CreateTime
	}

	return types.EIPInstance{
		AllocationID:       allocationID,
		IPAddress:          publicIP,
		Name:               "",
		Status:             status,
		Region:             region,
		Bandwidth:          bandwidth,
		InternetChargeType: chargeType,
		InstanceID:         instanceID,
		InstanceType:       instanceType,
		CreationTime:       createTime,
		Tags:               make(map[string]string),
		Provider:           "huawei",
	}
}

// convertDetailToEIPInstance 转换详情为通用EIP实例
func (a *EIPAdapter) convertDetailToEIPInstance(e model.PublicipShowResp, region string) types.EIPInstance {
	return a.convertToEIPInstance(e, region)
}
