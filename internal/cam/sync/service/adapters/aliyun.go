package adapters

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/gotomicro/ego/core/elog"
)

// AliyunAdapter 阿里云适配器
type AliyunAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string // 默认地域，用于获取地域列表等全局操作
	logger          *elog.Component
	clients         map[string]*ecs.Client // region -> client
}

// AliyunConfig 阿里云配置
type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	DefaultRegion   string // 默认地域，如果为空则使用 cn-shenzhen
}

// NewAliyunAdapter 创建阿里云适配器
func NewAliyunAdapter(config AliyunConfig, logger *elog.Component) *AliyunAdapter {
	// 设置默认地域
	defaultRegion := config.DefaultRegion
	if defaultRegion == "" {
		defaultRegion = "cn-shenzhen"
	}

	return &AliyunAdapter{
		accessKeyID:     config.AccessKeyID,
		accessKeySecret: config.AccessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
		clients:         make(map[string]*ecs.Client),
	}
}

// GetProvider 获取云厂商类型
func (a *AliyunAdapter) GetProvider() domain.CloudProvider {
	return domain.ProviderAliyun
}

// ValidateCredentials 验证凭证
func (a *AliyunAdapter) ValidateCredentials(ctx context.Context) error {
	a.logger.Info("验证阿里云凭证")

	// 尝试获取地域列表来验证凭证
	_, err := a.GetRegions(ctx)
	if err != nil {
		return fmt.Errorf("阿里云凭证验证失败: %w", err)
	}

	a.logger.Info("阿里云凭证验证成功")
	return nil
}

// GetRegions 获取支持的地域列表
func (a *AliyunAdapter) GetRegions(ctx context.Context) ([]domain.Region, error) {
	a.logger.Info("获取阿里云地域列表", elog.String("default_region", a.defaultRegion))

	// 使用默认地域创建客户端
	client, err := a.getClient(a.defaultRegion)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	// 创建请求
	request := ecs.CreateDescribeRegionsRequest()
	request.Scheme = "https"

	// 发送请求
	response, err := client.DescribeRegions(request)
	if err != nil {
		return nil, fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 转换结果
	regions := make([]domain.Region, 0, len(response.Regions.Region))
	for _, r := range response.Regions.Region {
		regions = append(regions, domain.Region{
			ID:          r.RegionId,
			Name:        r.RegionId,
			LocalName:   r.LocalName,
			Description: r.LocalName,
		})
	}

	a.logger.Info("获取阿里云地域列表成功", elog.Int("count", len(regions)))
	return regions, nil
}

// GetECSInstances 获取云主机实例列表
func (a *AliyunAdapter) GetECSInstances(ctx context.Context, region string) ([]domain.ECSInstance, error) {
	a.logger.Info("获取阿里云ECS实例列表", elog.String("region", region))

	client, err := a.getClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	var allInstances []domain.ECSInstance
	pageNumber := 1
	pageSize := 100

	for {
		// 创建请求
		request := ecs.CreateDescribeInstancesRequest()
		request.Scheme = "https"
		request.RegionId = region
		request.PageNumber = requests.NewInteger(pageNumber)
		request.PageSize = requests.NewInteger(pageSize)

		// 发送请求
		response, err := client.DescribeInstances(request)
		if err != nil {
			return nil, fmt.Errorf("获取实例列表失败: %w", err)
		}

		// 转换结果
		for _, inst := range response.Instances.Instance {
			instance := a.convertInstance(inst, region)
			allInstances = append(allInstances, instance)
		}

		// 检查是否还有更多数据
		if len(response.Instances.Instance) < pageSize {
			break
		}
		pageNumber++
	}

	a.logger.Info("获取阿里云ECS实例列表成功",
		elog.String("region", region),
		elog.Int("count", len(allInstances)))

	return allInstances, nil
}

// getClient 获取或创建指定地域的客户端
func (a *AliyunAdapter) getClient(region string) (*ecs.Client, error) {
	// 检查缓存存
	if client, ok := a.clients[region]; ok {
		return client, nil
	}

	// 创建凭证
	credential := credentials.NewAccessKeyCredential(a.accessKeyID, a.accessKeySecret)

	// 创建配置
	config := sdk.NewConfig()
	config.Scheme = "https"

	// 创建客户端
	client, err := ecs.NewClientWithOptions(region, config, credential)
	if err != nil {
		return nil, fmt.Errorf("创建ECS客户端失败: %w", err)
	}

	// 缓存存客户端
	a.clients[region] = client

	return client, nil
}

// convertInstance 转换阿里云实例为通用格式
func (a *AliyunAdapter) convertInstance(inst ecs.Instance, region string) domain.ECSInstance {
	// 获取公网IP
	publicIP := ""
	if len(inst.PublicIpAddress.IpAddress) > 0 {
		publicIP = inst.PublicIpAddress.IpAddress[0]
	}
	// 如果没有公网IP，尝试获取EIP
	if publicIP == "" && len(inst.EipAddress.IpAddress) > 0 {
		publicIP = inst.EipAddress.IpAddress
	}

	// 获取私网IP
	privateIP := ""
	if len(inst.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
		privateIP = inst.VpcAttributes.PrivateIpAddress.IpAddress[0]
	}

	// 转换安全组
	securityGroups := make([]string, 0, len(inst.SecurityGroupIds.SecurityGroupId))
	for _, sg := range inst.SecurityGroupIds.SecurityGroupId {
		securityGroups = append(securityGroups, sg)
	}

	// 转换数据盘（如果有的话）
	dataDisks := make([]domain.DataDisk, 0)
	// 注意：基本的 DescribeInstances 可能不包含详细的磁盘信息
	// 需要单独调用 DescribeDisks 获取

	// 转换标签
	tags := make(map[string]string)
	for _, tag := range inst.Tags.Tag {
		tags[tag.TagKey] = tag.TagValue
	}

	// 获取实例规格族
	instanceTypeFamily := ""
	if len(inst.InstanceType) > 0 {
		// 实例规格族通常是实例类型的前缀，如 ecs.g6.large -> g6
		parts := inst.InstanceType
		if len(parts) > 4 && parts[:4] == "ecs." {
			// 提取规格族，如 ecs.g6.large -> g6
			remaining := parts[4:]
			for i, c := range remaining {
				if c == '.' {
					instanceTypeFamily = remaining[:i]
					break
				}
			}
		}
	}

	return domain.ECSInstance{
		// 基本信息
		InstanceID:   inst.InstanceId,
		InstanceName: inst.InstanceName,
		Status:       inst.Status,
		Region:       region,
		Zone:         inst.ZoneId,

		// 配置信息
		InstanceType:       inst.InstanceType,
		InstanceTypeFamily: instanceTypeFamily,
		CPU:                inst.Cpu,
		Memory:             inst.Memory,
		OSType:             inst.OSType,
		OSName:             inst.OSName,
		ImageID:            inst.ImageId,

		// 网络信息
		PublicIP:                publicIP,
		PrivateIP:               privateIP,
		VPCID:                   inst.VpcAttributes.VpcId,
		VSwitchID:               inst.VpcAttributes.VSwitchId,
		SecurityGroups:          securityGroups,
		InternetMaxBandwidthIn:  inst.InternetMaxBandwidthIn,
		InternetMaxBandwidthOut: inst.InternetMaxBandwidthOut,

		// 存储信息
		SystemDiskCategory: "", // 需要单独获取
		SystemDiskSize:     0,  // 需要单独获取
		DataDisks:          dataDisks,

		// 计费信息
		ChargeType:      inst.InstanceChargeType,
		CreationTime:    inst.CreationTime,
		ExpiredTime:     inst.ExpiredTime,
		AutoRenew:       false, // 需要单独获取
		AutoRenewPeriod: 0,     // 需要单独获取

		// 监控信息
		IoOptimized:         boolToString(inst.IoOptimized),
		NetworkType:         inst.NetworkType,
		InstanceNetworkType: inst.InstanceNetworkType,

		// 其他信息
		Tags:        tags,
		Description: inst.Description,
		Provider:    string(domain.ProviderAliyun),
		HostName:    inst.HostName,
		KeyPairName: inst.KeyPairName,
	}
}

// GetECSInstanceDetail 获取单个云主机实例的详细信息
func (a *AliyunAdapter) GetECSInstanceDetail(ctx context.Context, region, instanceID string) (*domain.ECSInstance, error) {
	a.logger.Info("获取阿里云ECS实例详情",
		elog.String("region", region),
		elog.String("instance_id", instanceID))

	client, err := a.getClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	// 创建请求
	request := ecs.CreateDescribeInstancesRequest()
	request.Scheme = "https"
	request.RegionId = region
	request.InstanceIds = fmt.Sprintf("[\"%s\"]", instanceID)

	// 发送请求
	response, err := client.DescribeInstances(request)
	if err != nil {
		return nil, fmt.Errorf("获取实例详情失败: %w", err)
	}

	if len(response.Instances.Instance) == 0 {
		return nil, fmt.Errorf("实例不存在: %s", instanceID)
	}

	// 转换结果
	instance := a.convertInstance(response.Instances.Instance[0], region)

	a.logger.Info("获取阿里云ECS实例详情成功",
		elog.String("region", region),
		elog.String("instance_id", instanceID))

	return &instance, nil
}

// GetInstanceTypes 获取实例规格信息
func (a *AliyunAdapter) GetInstanceTypes(ctx context.Context, region string) ([]InstanceType, error) {
	a.logger.Info("获取阿里云实例规格列表", elog.String("region", region))

	client, err := a.getClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	// 创建请求
	request := ecs.CreateDescribeInstanceTypesRequest()
	request.Scheme = "https"

	// 发送请求
	response, err := client.DescribeInstanceTypes(request)
	if err != nil {
		return nil, fmt.Errorf("获取实例规格列表失败: %w", err)
	}

	// 转换结果
	instanceTypes := make([]InstanceType, 0, len(response.InstanceTypes.InstanceType))
	for _, it := range response.InstanceTypes.InstanceType {
		instanceTypes = append(instanceTypes, InstanceType{
			InstanceTypeID:      it.InstanceTypeId,
			InstanceTypeFamily:  it.InstanceTypeFamily,
			CPUCoreCount:        it.CpuCoreCount,
			MemorySize:          it.MemorySize,
			InstanceBandwidthRx: it.InstanceBandwidthRx,
			InstanceBandwidthTx: it.InstanceBandwidthTx,
			InstancePpsRx:       int(it.InstancePpsRx),
			InstancePpsTx:       int(it.InstancePpsTx),
		})
	}

	a.logger.Info("获取阿里云实例规格列表成功",
		elog.String("region", region),
		elog.Int("count", len(instanceTypes)))

	return instanceTypes, nil
}

// InstanceType 实例规格信息
type InstanceType struct {
	InstanceTypeID      string  `json:"instance_type_id"`
	InstanceTypeFamily  string  `json:"instance_type_family"`
	CPUCoreCount        int     `json:"cpu_core_count"`
	MemorySize          float64 `json:"memory_size"`           // GB
	InstanceBandwidthRx int     `json:"instance_bandwidth_rx"` // Kbps
	InstanceBandwidthTx int     `json:"instance_bandwidth_tx"` // Kbps
	InstancePpsRx       int     `json:"instance_pps_rx"`       // 包/秒
	InstancePpsTx       int     `json:"instance_pps_tx"`       // 包/秒
}

// boolToString 将布尔值转换为字符串
func boolToString(b bool) string {
	if b {
		return "optimized"
	}
	return "none"
}

// GetInstanceDisks 获取实例的磁盘信息
func (a *AliyunAdapter) GetInstanceDisks(ctx context.Context, region, instanceID string) ([]domain.DataDisk, error) {
	a.logger.Info("获取阿里云ECS实例磁盘信息",
		elog.String("region", region),
		elog.String("instance_id", instanceID))

	client, err := a.getClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	// 创建请求
	request := ecs.CreateDescribeDisksRequest()
	request.Scheme = "https"
	request.RegionId = region
	request.InstanceId = instanceID

	// 发送请求
	response, err := client.DescribeDisks(request)
	if err != nil {
		return nil, fmt.Errorf("获取磁盘信息失败: %w", err)
	}

	// 转换结果
	disks := make([]domain.DataDisk, 0, len(response.Disks.Disk))
	for _, disk := range response.Disks.Disk {
		// 跳过系统盘
		if disk.Type == "system" {
			continue
		}

		disks = append(disks, domain.DataDisk{
			DiskID:   disk.DiskId,
			Category: disk.Category,
			Size:     disk.Size,
			Device:   disk.Device,
		})
	}

	a.logger.Info("获取阿里云ECS实例磁盘信息成功",
		elog.String("region", region),
		elog.String("instance_id", instanceID),
		elog.Int("disk_count", len(disks)))

	return disks, nil
}

// GetInstancesWithDetails 获取云主机实例列表（包含详细信息）
func (a *AliyunAdapter) GetInstancesWithDetails(ctx context.Context, region string) ([]domain.ECSInstance, error) {
	a.logger.Info("获取阿里云ECS实例列表（含详细信息）", elog.String("region", region))

	// 先获取基本实例列表
	instances, err := a.GetECSInstances(ctx, region)
	if err != nil {
		return nil, err
	}

	// 为每个实例获取详细信息（磁盘信息）
	for i := range instances {
		// 获取磁盘信息
		disks, err := a.GetInstanceDisks(ctx, region, instances[i].InstanceID)
		if err != nil {
			a.logger.Warn("获取实例磁盘信息失败",
				elog.String("instance_id", instances[i].InstanceID),
				elog.FieldErr(err))
			continue
		}
		instances[i].DataDisks = disks
	}

	a.logger.Info("获取阿里云ECS实例列表（含详细信息）成功",
		elog.String("region", region),
		elog.Int("count", len(instances)))

	return instances, nil
}

// GetInstanceMonitorData 获取实例监控数据
func (a *AliyunAdapter) GetInstanceMonitorData(ctx context.Context, region, instanceID string, startTime, endTime string) (*InstanceMonitorData, error) {
	a.logger.Info("获取阿里云ECS实例监控数据",
		elog.String("region", region),
		elog.String("instance_id", instanceID))

	client, err := a.getClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}

	// 创建请求
	request := ecs.CreateDescribeInstanceMonitorDataRequest()
	request.Scheme = "https"
	request.InstanceId = instanceID
	request.StartTime = startTime
	request.EndTime = endTime

	// 发送请求
	response, err := client.DescribeInstanceMonitorData(request)
	if err != nil {
		return nil, fmt.Errorf("获取监控数据失败: %w", err)
	}

	// 转换结果
	monitorData := &InstanceMonitorData{
		InstanceID: instanceID,
		DataPoints: make([]MonitorDataPoint, 0, len(response.MonitorData.InstanceMonitorData)),
	}

	for _, data := range response.MonitorData.InstanceMonitorData {
		monitorData.DataPoints = append(monitorData.DataPoints, MonitorDataPoint{
			Timestamp:            data.TimeStamp,
			CPUUtilization:       float64(data.CPU),
			MemoryUtilization:    0, // 阿里云API可能不提供内存使用率
			InternetBandwidthIn:  data.InternetRX,
			InternetBandwidthOut: data.InternetTX,
			IntranetBandwidthIn:  data.IntranetRX,
			IntranetBandwidthOut: data.IntranetTX,
			IOPSRead:             data.IOPSRead,
			IOPSWrite:            data.IOPSWrite,
			BPSRead:              data.BPSRead,
			BPSWrite:             data.BPSWrite,
		})
	}

	a.logger.Info("获取阿里云ECS实例监控数据成功",
		elog.String("region", region),
		elog.String("instance_id", instanceID),
		elog.Int("data_points", len(monitorData.DataPoints)))

	return monitorData, nil
}

// InstanceMonitorData 实例监控数据
type InstanceMonitorData struct {
	InstanceID string
	DataPoints []MonitorDataPoint
}

// MonitorDataPoint 监控数据点
type MonitorDataPoint struct {
	Timestamp            string
	CPUUtilization       float64
	MemoryUtilization    float64
	InternetBandwidthIn  int
	InternetBandwidthOut int
	IntranetBandwidthIn  int
	IntranetBandwidthOut int
	IOPSRead             int
	IOPSWrite            int
	BPSRead              int
	BPSWrite             int
}
