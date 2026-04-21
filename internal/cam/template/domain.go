package template

// ============================================================================
// 领域模型
// ============================================================================

// VMTemplate 主机模板
type VMTemplate struct {
	ID          int64  `bson:"id" json:"id"`
	Name        string `bson:"name" json:"name"`
	Description string `bson:"description" json:"description"`
	TenantID    string `bson:"tenant_id" json:"tenant_id"`

	// 云厂商信息
	Provider       string `bson:"provider" json:"provider"`
	CloudAccountID int64  `bson:"cloud_account_id" json:"cloud_account_id"`

	// 必填参数
	Region           string   `bson:"region" json:"region"`
	Zone             string   `bson:"zone" json:"zone"`
	InstanceType     string   `bson:"instance_type" json:"instance_type"`
	ImageID          string   `bson:"image_id" json:"image_id"`
	VPCID            string   `bson:"vpc_id" json:"vpc_id"`
	SubnetID         string   `bson:"subnet_id" json:"subnet_id"`
	SecurityGroupIDs []string `bson:"security_group_ids" json:"security_group_ids"`

	// 可选参数
	InstanceNamePrefix string            `bson:"instance_name_prefix" json:"instance_name_prefix"`
	HostNamePrefix     string            `bson:"host_name_prefix" json:"host_name_prefix"`
	SystemDiskType     string            `bson:"system_disk_type" json:"system_disk_type"`
	SystemDiskSize     int               `bson:"system_disk_size" json:"system_disk_size"`
	DataDisks          []DataDiskConfig  `bson:"data_disks" json:"data_disks"`
	BandwidthOut       int               `bson:"bandwidth_out" json:"bandwidth_out"`
	ChargeType         string            `bson:"charge_type" json:"charge_type"`
	KeyPairName        string            `bson:"key_pair_name" json:"key_pair_name"`
	Tags               map[string]string `bson:"tags" json:"tags"`

	Ctime int64 `bson:"ctime" json:"created_at"`
	Utime int64 `bson:"utime" json:"updated_at"`
}

// DataDiskConfig 数据盘配置
type DataDiskConfig struct {
	Category string `bson:"category" json:"category"`
	Size     int    `bson:"size" json:"size"`
}

// ============================================================================
// 创建任务相关
// ============================================================================

// 创建方式常量
const (
	SourceFromTemplate = "from_template" // 按模板创建
	SourceDirect       = "direct"        // 直接创建
)

// 任务状态常量
const (
	TaskStatusPending        = "pending"
	TaskStatusRunning        = "running"
	TaskStatusSuccess        = "success"
	TaskStatusPartialSuccess = "partial_success"
	TaskStatusFailed         = "failed"
)

// 同步状态常量
const (
	SyncStatusPending = "pending"
	SyncStatusSyncing = "syncing"
	SyncStatusSynced  = "synced"
	SyncStatusFailed  = "failed"
)

// ProvisionTask 创建任务
type ProvisionTask struct {
	ID       string `bson:"_id" json:"id"`
	TenantID string `bson:"tenant_id" json:"tenant_id"`

	// 创建来源
	Source     string `bson:"source" json:"source"`
	TemplateID int64  `bson:"template_id" json:"template_id"`

	// 直接创建时保存的参数快照
	DirectParams *DirectProvisionParams `bson:"direct_params,omitempty" json:"direct_params,omitempty"`

	// 创建参数
	Count              int               `bson:"count" json:"count"`
	InstanceNamePrefix string            `bson:"instance_name_prefix" json:"instance_name_prefix"`
	OverrideTags       map[string]string `bson:"override_tags" json:"override_tags"`

	// 状态
	Status   string `bson:"status" json:"status"`
	Progress int    `bson:"progress" json:"progress"`
	Message  string `bson:"message" json:"message"`

	// 结果
	SuccessCount int                       `bson:"success_count" json:"success_count"`
	FailedCount  int                       `bson:"failed_count" json:"failed_count"`
	Instances    []ProvisionInstanceResult `bson:"instances" json:"instances"`

	// 资产同步状态
	SyncStatus string `bson:"sync_status" json:"sync_status"`

	CreatedBy string `bson:"created_by" json:"created_by"`
	Ctime     int64  `bson:"ctime" json:"created_at"`
	Utime     int64  `bson:"utime" json:"updated_at"`
}

// ProvisionInstanceResult 单台实例创建结果
type ProvisionInstanceResult struct {
	Index      int    `bson:"index" json:"index"`
	Name       string `bson:"name" json:"name"`
	InstanceID string `bson:"instance_id" json:"instance_id"`
	Status     string `bson:"status" json:"status"`
	Error      string `bson:"error" json:"error"`
	SyncStatus string `bson:"sync_status" json:"sync_status"`
}

// DirectProvisionParams 直接创建时保存的参数快照
type DirectProvisionParams struct {
	Provider           string            `bson:"provider" json:"provider"`
	CloudAccountID     int64             `bson:"cloud_account_id" json:"cloud_account_id"`
	Region             string            `bson:"region" json:"region"`
	Zone               string            `bson:"zone" json:"zone"`
	InstanceType       string            `bson:"instance_type" json:"instance_type"`
	ImageID            string            `bson:"image_id" json:"image_id"`
	VPCID              string            `bson:"vpc_id" json:"vpc_id"`
	SubnetID           string            `bson:"subnet_id" json:"subnet_id"`
	SecurityGroupIDs   []string          `bson:"security_group_ids" json:"security_group_ids"`
	InstanceNamePrefix string            `bson:"instance_name_prefix" json:"instance_name_prefix"`
	HostNamePrefix     string            `bson:"host_name_prefix" json:"host_name_prefix"`
	SystemDiskType     string            `bson:"system_disk_type" json:"system_disk_type"`
	SystemDiskSize     int               `bson:"system_disk_size" json:"system_disk_size"`
	DataDisks          []DataDiskConfig  `bson:"data_disks" json:"data_disks"`
	BandwidthOut       int               `bson:"bandwidth_out" json:"bandwidth_out"`
	ChargeType         string            `bson:"charge_type" json:"charge_type"`
	KeyPairName        string            `bson:"key_pair_name" json:"key_pair_name"`
	Tags               map[string]string `bson:"tags" json:"tags"`
	Description        string            `bson:"description" json:"description"`
}

// ============================================================================
// 请求/响应结构体
// ============================================================================

// CreateTemplateReq 创建模板请求
type CreateTemplateReq struct {
	Name               string            `json:"name" binding:"required"`
	Description        string            `json:"description"`
	Provider           string            `json:"provider"`
	CloudAccountID     int64             `json:"cloud_account_id"`
	Region             string            `json:"region"`
	Zone               string            `json:"zone"`
	InstanceType       string            `json:"instance_type"`
	ImageID            string            `json:"image_id"`
	VPCID              string            `json:"vpc_id"`
	SubnetID           string            `json:"subnet_id"`
	SecurityGroupIDs   []string          `json:"security_group_ids"`
	InstanceNamePrefix string            `json:"instance_name_prefix"`
	HostNamePrefix     string            `json:"host_name_prefix"`
	SystemDiskType     string            `json:"system_disk_type"`
	SystemDiskSize     int               `json:"system_disk_size"`
	DataDisks          []DataDiskConfig  `json:"data_disks"`
	BandwidthOut       int               `json:"bandwidth_out"`
	ChargeType         string            `json:"charge_type"`
	KeyPairName        string            `json:"key_pair_name"`
	Tags               map[string]string `json:"tags"`
}

// UpdateTemplateReq 更新模板请求
type UpdateTemplateReq struct {
	Name               *string            `json:"name"`
	Description        *string            `json:"description"`
	Provider           *string            `json:"provider"`
	CloudAccountID     *int64             `json:"cloud_account_id"`
	Region             *string            `json:"region"`
	Zone               *string            `json:"zone"`
	InstanceType       *string            `json:"instance_type"`
	ImageID            *string            `json:"image_id"`
	VPCID              *string            `json:"vpc_id"`
	SubnetID           *string            `json:"subnet_id"`
	SecurityGroupIDs   *[]string          `json:"security_group_ids"`
	InstanceNamePrefix *string            `json:"instance_name_prefix"`
	HostNamePrefix     *string            `json:"host_name_prefix"`
	SystemDiskType     *string            `json:"system_disk_type"`
	SystemDiskSize     *int               `json:"system_disk_size"`
	DataDisks          *[]DataDiskConfig  `json:"data_disks"`
	BandwidthOut       *int               `json:"bandwidth_out"`
	ChargeType         *string            `json:"charge_type"`
	KeyPairName        *string            `json:"key_pair_name"`
	Tags               *map[string]string `json:"tags"`
}

// ProvisionReq 基于模板创建主机请求
type ProvisionReq struct {
	Count              int               `json:"count" binding:"required,min=1,max=20"`
	InstanceNamePrefix string            `json:"instance_name_prefix"`
	Tags               map[string]string `json:"tags"`
}

// DirectProvisionReq 直接创建主机请求
type DirectProvisionReq struct {
	Provider           string            `json:"provider" binding:"required"`
	CloudAccountID     int64             `json:"cloud_account_id" binding:"required"`
	Region             string            `json:"region" binding:"required"`
	Zone               string            `json:"zone"`
	InstanceType       string            `json:"instance_type" binding:"required"`
	ImageID            string            `json:"image_id"`
	VPCID              string            `json:"vpc_id"`
	SubnetID           string            `json:"subnet_id"`
	SecurityGroupIDs   []string          `json:"security_group_ids"`
	Count              int               `json:"count" binding:"required,min=1,max=20"`
	InstanceNamePrefix string            `json:"instance_name_prefix"`
	HostNamePrefix     string            `json:"host_name_prefix"`
	SystemDiskType     string            `json:"system_disk_type"`
	SystemDiskSize     int               `json:"system_disk_size"`
	DataDisks          []DataDiskConfig  `json:"data_disks"`
	BandwidthOut       int               `json:"bandwidth_out"`
	ChargeType         string            `json:"charge_type"`
	KeyPairName        string            `json:"key_pair_name"`
	Tags               map[string]string `json:"tags"`
	Description        string            `json:"description"`
}

// ============================================================================
// 查询过滤条件
// ============================================================================

// TemplateFilter 模板查询过滤条件
type TemplateFilter struct {
	TenantID       string `json:"tenant_id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	CloudAccountID int64  `json:"cloud_account_id"`
	Offset         int64  `json:"offset"`
	Limit          int64  `json:"limit"`
}

// ProvisionTaskFilter 创建任务查询过滤条件
type ProvisionTaskFilter struct {
	TenantID   string `json:"tenant_id"`
	TemplateID int64  `json:"template_id"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	StartTime  int64  `json:"start_time"`
	EndTime    int64  `json:"end_time"`
	Offset     int64  `json:"offset"`
	Limit      int64  `json:"limit"`
}
