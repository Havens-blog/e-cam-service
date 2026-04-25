package types

// ENIInstance 弹性网卡实例（通用格式）
type ENIInstance struct {
	// 基本信息
	ENIID       string `json:"eni_id"`      // 弹性网卡ID
	ENIName     string `json:"eni_name"`    // 弹性网卡名称
	Description string `json:"description"` // 描述
	Status      string `json:"status"`      // 状态
	Type        string `json:"type"`        // 网卡类型: Primary(主网卡), Secondary(辅助网卡)
	Region      string `json:"region"`      // 地域
	Zone        string `json:"zone"`        // 可用区

	// 网络信息
	VPCID              string   `json:"vpc_id"`               // VPC ID
	SubnetID           string   `json:"subnet_id"`            // 子网/交换机ID
	PrimaryPrivateIP   string   `json:"primary_private_ip"`   // 主私网IP
	PrivateIPAddresses []string `json:"private_ip_addresses"` // 所有私网IP列表
	MacAddress         string   `json:"mac_address"`          // MAC地址
	IPv6Addresses      []string `json:"ipv6_addresses"`       // IPv6地址列表

	// 绑定信息
	InstanceID   string `json:"instance_id"`   // 绑定的ECS实例ID
	InstanceName string `json:"instance_name"` // 绑定的ECS实例名称
	DeviceIndex  int    `json:"device_index"`  // 设备索引

	// 安全组
	SecurityGroupIDs []string `json:"security_group_ids"` // 关联的安全组ID列表

	// 公网信息
	PublicIP     string   `json:"public_ip"`     // 关联的公网IP
	EIPAddresses []string `json:"eip_addresses"` // 关联的EIP地址列表

	// 资源信息
	ResourceGroupID string `json:"resource_group_id"` // 资源组ID
	ProjectID       string `json:"project_id"`        // 项目ID

	// 计费信息
	CreationTime string `json:"creation_time"` // 创建时间

	// 云账号信息
	CloudAccountID   int64  `json:"cloud_account_id"`
	CloudAccountName string `json:"cloud_account_name"`

	// 其他信息
	Tags     map[string]string `json:"tags"`
	Provider string            `json:"provider"` // 云厂商标识
}

// ENIInstanceFilter ENI实例过滤条件
type ENIInstanceFilter struct {
	ENIIDs           []string          `json:"eni_ids,omitempty"`
	ENIName          string            `json:"eni_name,omitempty"`
	Status           []string          `json:"status,omitempty"`
	Type             string            `json:"type,omitempty"` // Primary / Secondary
	VPCID            string            `json:"vpc_id,omitempty"`
	SubnetID         string            `json:"subnet_id,omitempty"`
	InstanceID       string            `json:"instance_id,omitempty"` // 绑定的ECS实例ID
	PrimaryPrivateIP string            `json:"primary_private_ip,omitempty"`
	SecurityGroupID  string            `json:"security_group_id,omitempty"` // 关联的安全组ID
	Tags             map[string]string `json:"tags,omitempty"`
	PageNumber       int               `json:"page_number,omitempty"`
	PageSize         int               `json:"page_size,omitempty"`
}

// ENI 网卡类型常量
const (
	ENITypePrimary   = "Primary"   // 主网卡
	ENITypeSecondary = "Secondary" // 辅助网卡
)

// ENI 标准化状态
const (
	ENIStatusAvailable = "available" // 可用 (未绑定)
	ENIStatusInUse     = "in_use"    // 使用中 (已绑定)
	ENIStatusAttaching = "attaching" // 绑定中
	ENIStatusDetaching = "detaching" // 解绑中
	ENIStatusCreating  = "creating"  // 创建中
	ENIStatusDeleting  = "deleting"  // 删除中
	ENIStatusError     = "error"     // 异常
	ENIStatusUnknown   = "unknown"   // 未知
)

// NormalizeENIStatus 标准化弹性网卡状态
func NormalizeENIStatus(provider, status string) string {
	if status == "" {
		return ENIStatusUnknown
	}

	switch provider {
	case "aliyun":
		return normalizeAliyunENIStatus(status)
	case "aws":
		return normalizeAWSENIStatus(status)
	case "huawei":
		return normalizeHuaweiENIStatus(status)
	case "tencent":
		return normalizeTencentENIStatus(status)
	case "volcano", "volcengine":
		return normalizeVolcanoENIStatus(status)
	default:
		return status
	}
}

func normalizeAliyunENIStatus(status string) string {
	switch status {
	case "Available":
		return ENIStatusAvailable
	case "InUse":
		return ENIStatusInUse
	case "Attaching":
		return ENIStatusAttaching
	case "Detaching":
		return ENIStatusDetaching
	case "Creating":
		return ENIStatusCreating
	case "Deleting":
		return ENIStatusDeleting
	default:
		return status
	}
}

func normalizeAWSENIStatus(status string) string {
	switch status {
	case "available":
		return ENIStatusAvailable
	case "in-use":
		return ENIStatusInUse
	case "attaching":
		return ENIStatusAttaching
	case "detaching":
		return ENIStatusDetaching
	case "associated":
		return ENIStatusInUse
	default:
		return status
	}
}

func normalizeHuaweiENIStatus(status string) string {
	switch status {
	case "ACTIVE":
		return ENIStatusInUse
	case "BUILD":
		return ENIStatusCreating
	case "DOWN":
		return ENIStatusAvailable
	case "ERROR":
		return ENIStatusError
	default:
		return status
	}
}

func normalizeTencentENIStatus(status string) string {
	switch status {
	case "AVAILABLE":
		return ENIStatusAvailable
	case "BINDbindingd", "BINDbindingd ":
		return ENIStatusAttaching
	case "BINDUNBINDING":
		return ENIStatusDetaching
	case "BINDBOUND":
		return ENIStatusInUse
	case "BINDUNBOUND":
		return ENIStatusAvailable
	case "BINDDELETING":
		return ENIStatusDeleting
	// 腾讯云 Pending 状态
	case "PENDING":
		return ENIStatusCreating
	case "DELETING":
		return ENIStatusDeleting
	default:
		return status
	}
}

func normalizeVolcanoENIStatus(status string) string {
	switch status {
	case "Available":
		return ENIStatusAvailable
	case "InUse":
		return ENIStatusInUse
	case "Attaching":
		return ENIStatusAttaching
	case "Detaching":
		return ENIStatusDetaching
	case "Creating":
		return ENIStatusCreating
	case "Deleting":
		return ENIStatusDeleting
	default:
		return status
	}
}
