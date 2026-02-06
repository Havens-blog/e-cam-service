// Package domain 同步服务领域模型
package domain

import "context"

// CloudProvider 云厂商类型
type CloudProvider string

const (
	ProviderAliyun  CloudProvider = "aliyun"
	ProviderAWS     CloudProvider = "aws"
	ProviderAzure   CloudProvider = "azure"
	ProviderHuawei  CloudProvider = "huawei"
	ProviderVolcano CloudProvider = "volcano"
	ProviderTencent CloudProvider = "tencent"
)

// CloudAdapter 云厂商适配器接口
type CloudAdapter interface {
	// GetProvider 获取云厂商类型
	GetProvider() CloudProvider

	// ValidateCredentials 验证凭证
	ValidateCredentials(ctx context.Context) error

	// GetECSInstances 获取云主机实例列表
	GetECSInstances(ctx context.Context, region string) ([]ECSInstance, error)

	// GetRegions 获取支持的地域列表
	GetRegions(ctx context.Context) ([]Region, error)
}

// Region 地域信息
type Region struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	LocalName   string `json:"local_name"`
	Description string `json:"description"`
}

// ECSInstance 云主机实例（通用格式）
type ECSInstance struct {
	// 基本信息
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Status       string `json:"status"`
	Region       string `json:"region"`
	Zone         string `json:"zone"`

	// 配置信息
	InstanceType       string `json:"instance_type"`
	InstanceTypeFamily string `json:"instance_type_family"`
	CPU                int    `json:"cpu"`
	Memory             int    `json:"memory"` // MB
	OSType             string `json:"os_type"`
	OSName             string `json:"os_name"`
	ImageID            string `json:"image_id"`

	// 网络信息
	PublicIP                string   `json:"public_ip"`
	PrivateIP               string   `json:"private_ip"`
	VPCID                   string   `json:"vpc_id"`
	VSwitchID               string   `json:"vswitch_id"`
	SecurityGroups          []string `json:"security_groups"`
	InternetMaxBandwidthIn  int      `json:"internet_max_bandwidth_in"`
	InternetMaxBandwidthOut int      `json:"internet_max_bandwidth_out"`

	// 存储信息
	SystemDiskCategory string     `json:"system_disk_category"`
	SystemDiskSize     int        `json:"system_disk_size"` // GB
	DataDisks          []DataDisk `json:"data_disks"`

	// 计费信息
	ChargeType      string `json:"charge_type"`
	CreationTime    string `json:"creation_time"`
	ExpiredTime     string `json:"expired_time"`
	AutoRenew       bool   `json:"auto_renew"`
	AutoRenewPeriod int    `json:"auto_renew_period"`

	// 监控信息
	IoOptimized         string `json:"io_optimized"`
	NetworkType         string `json:"network_type"`
	InstanceNetworkType string `json:"instance_network_type"`

	// 其他信息
	Tags        map[string]string `json:"tags"`
	Description string            `json:"description"`
	Provider    string            `json:"provider"` // 云厂商标识
	HostName    string            `json:"host_name"`
	KeyPairName string            `json:"key_pair_name"`
}

// DataDisk 数据盘信息
type DataDisk struct {
	DiskID   string `json:"disk_id"`
	Category string `json:"category"`
	Size     int    `json:"size"` // GB
	Device   string `json:"device"`
}
