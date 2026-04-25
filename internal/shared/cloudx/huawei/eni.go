package huawei

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	vpcSdk "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

// ENIAdapter 华为云弹性网卡适配器
type ENIAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewENIAdapter 创建弹性网卡适配器
func NewENIAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *ENIAdapter {
	return &ENIAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建VPC客户端（ENI在华为云属于VPC Port服务）
func (a *ENIAdapter) createClient(region string) (*vpcSdk.VpcClient, error) {
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

	r, err := vpcregion.SafeValueOf(region)
	if err != nil {
		return nil, fmt.Errorf("无效的地域: %s", region)
	}

	client, err := vpcSdk.VpcClientBuilder().
		WithRegion(r).
		WithCredential(auth).
		SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("创建华为云VPC客户端失败: %w", err)
	}

	return vpcSdk.NewVpcClient(client), nil
}

// ListInstances 获取弹性网卡列表
func (a *ENIAdapter) ListInstances(ctx context.Context, region string) ([]types.ENIInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	var allENIs []types.ENIInstance
	var marker *string
	limit := int32(100)

	for {
		request := &model.ListPortsRequest{
			Limit:  &limit,
			Marker: marker,
		}

		response, err := client.ListPorts(request)
		if err != nil {
			return nil, fmt.Errorf("获取弹性网卡列表失败: %w", err)
		}

		if response.Ports == nil || len(*response.Ports) == 0 {
			break
		}

		for _, port := range *response.Ports {
			allENIs = append(allENIs, a.convertToENIInstance(port, region))
		}

		ports := *response.Ports
		if len(ports) < int(limit) {
			break
		}
		lastID := ports[len(ports)-1].Id
		marker = &lastID
	}

	a.logger.Info("获取华为云弹性网卡列表成功",
		elog.String("region", region),
		elog.Int("count", len(allENIs)))

	return allENIs, nil
}

// GetInstance 获取单个弹性网卡详情
func (a *ENIAdapter) GetInstance(ctx context.Context, region, eniID string) (*types.ENIInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := &model.ShowPortRequest{
		PortId: eniID,
	}

	response, err := client.ShowPort(request)
	if err != nil {
		return nil, fmt.Errorf("获取弹性网卡详情失败: %w", err)
	}

	if response.Port == nil {
		return nil, fmt.Errorf("弹性网卡不存在: %s", eniID)
	}

	instance := a.convertToENIInstance(*response.Port, region)
	return &instance, nil
}

// ListInstancesByIDs 批量获取弹性网卡
func (a *ENIAdapter) ListInstancesByIDs(ctx context.Context, region string, eniIDs []string) ([]types.ENIInstance, error) {
	var result []types.ENIInstance
	for _, id := range eniIDs {
		instance, err := a.GetInstance(ctx, region, id)
		if err != nil {
			a.logger.Warn("获取弹性网卡失败", elog.String("eni_id", id), elog.FieldErr(err))
			continue
		}
		result = append(result, *instance)
	}
	return result, nil
}

// GetInstanceStatus 获取弹性网卡状态
func (a *ENIAdapter) GetInstanceStatus(ctx context.Context, region, eniID string) (string, error) {
	instance, err := a.GetInstance(ctx, region, eniID)
	if err != nil {
		return "", err
	}
	return instance.Status, nil
}

// ListInstancesWithFilter 带过滤条件获取弹性网卡列表
func (a *ENIAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.ENIInstanceFilter) ([]types.ENIInstance, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	request := &model.ListPortsRequest{}

	if filter != nil {
		if len(filter.ENIIDs) > 0 {
			request.Id = &filter.ENIIDs
		}
		if filter.ENIName != "" {
			request.Name = &filter.ENIName
		}
		if len(filter.Status) > 0 {
			status := model.GetListPortsRequestStatusEnum().ACTIVE
			switch filter.Status[0] {
			case types.ENIStatusInUse:
				status = model.GetListPortsRequestStatusEnum().ACTIVE
			case types.ENIStatusAvailable:
				status = model.GetListPortsRequestStatusEnum().DOWN
			case types.ENIStatusCreating:
				status = model.GetListPortsRequestStatusEnum().BUILD
			}
			request.Status = &status
		}
		if filter.VPCID != "" {
			// 华为云 Port 没有直接的 VPC 过滤，需要通过 network_id (子网ID) 间接过滤
		}
		if filter.SubnetID != "" {
			request.NetworkId = &filter.SubnetID
		}
		if filter.InstanceID != "" {
			request.DeviceId = &filter.InstanceID
		}
		if filter.PageSize > 0 {
			limit := int32(filter.PageSize)
			request.Limit = &limit
		}
	}

	response, err := client.ListPorts(request)
	if err != nil {
		return nil, fmt.Errorf("获取弹性网卡列表失败: %w", err)
	}

	var result []types.ENIInstance
	if response.Ports != nil {
		for _, port := range *response.Ports {
			result = append(result, a.convertToENIInstance(port, region))
		}
	}

	return result, nil
}

// ListByInstanceID 获取实例绑定的弹性网卡
func (a *ENIAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.ENIInstance, error) {
	return a.ListInstancesWithFilter(ctx, region, &types.ENIInstanceFilter{
		InstanceID: instanceID,
	})
}

// convertToENIInstance 转换为通用弹性网卡实例
func (a *ENIAdapter) convertToENIInstance(port model.Port, region string) types.ENIInstance {
	// 提取私网IP列表
	var privateIPs []string
	var primaryIP string
	for _, ip := range port.FixedIps {
		if ip.IpAddress != nil && *ip.IpAddress != "" {
			privateIPs = append(privateIPs, *ip.IpAddress)
			if primaryIP == "" {
				primaryIP = *ip.IpAddress
			}
		}
	}

	// 提取安全组ID列表
	securityGroupIDs := port.SecurityGroups

	// 确定网卡类型
	eniType := types.ENITypeSecondary
	deviceOwner := port.DeviceOwner.Value()
	_ = deviceOwner

	// 提取子网ID
	subnetID := ""
	if len(port.FixedIps) > 0 && port.FixedIps[0].SubnetId != nil {
		subnetID = *port.FixedIps[0].SubnetId
	}

	// 提取状态
	status := port.Status.Value()

	// 提取实例ID
	instanceID := port.DeviceId

	// 提取名称
	name := port.Name

	macAddress := port.MacAddress

	return types.ENIInstance{
		ENIID:              port.Id,
		ENIName:            name,
		Status:             types.NormalizeENIStatus("huawei", status),
		Type:               eniType,
		Region:             region,
		SubnetID:           subnetID,
		PrimaryPrivateIP:   primaryIP,
		PrivateIPAddresses: privateIPs,
		MacAddress:         macAddress,
		InstanceID:         instanceID,
		SecurityGroupIDs:   securityGroupIDs,
		Tags:               make(map[string]string),
		Provider:           "huawei",
	}
}
