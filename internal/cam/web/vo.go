package web

import "time"

// Page 分页参数
type Page struct {
	Offset int64 `json:"offset,omitempty"`
	Limit  int64 `json:"limit,omitempty"`
}

// CloudAsset 云资产VO
type CloudAsset struct {
	Id           int64     `json:"id"`
	AssetId      string    `json:"asset_id"`
	AssetName    string    `json:"asset_name"`
	AssetType    string    `json:"asset_type"`
	Provider     string    `json:"provider"`
	Region       string    `json:"region"`
	Zone         string    `json:"zone"`
	Status       string    `json:"status"`
	Tags         []Tag     `json:"tags"`
	Metadata     string    `json:"metadata"`
	Cost         float64   `json:"cost"`
	CreateTime   time.Time `json:"create_time"`
	UpdateTime   time.Time `json:"update_time"`
	DiscoverTime time.Time `json:"discover_time"`
}

// Tag 标签VO
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreateAssetReq 创建资产请求
type CreateAssetReq struct {
	AssetId      string    `json:"asset_id" binding:"required"`
	AssetName    string    `json:"asset_name" binding:"required"`
	AssetType    string    `json:"asset_type" binding:"required"`
	Provider     string    `json:"provider" binding:"required"`
	Region       string    `json:"region" binding:"required"`
	Zone         string    `json:"zone"`
	Status       string    `json:"status"`
	Tags         []Tag     `json:"tags"`
	Metadata     string    `json:"metadata"`
	Cost         float64   `json:"cost"`
	CreateTime   time.Time `json:"create_time"`
	UpdateTime   time.Time `json:"update_time"`
	DiscoverTime time.Time `json:"discover_time"`
}

// CreateMultiAssetsReq 批量创建资产请求
type CreateMultiAssetsReq struct {
	Assets []CreateAssetReq `json:"assets" binding:"required"`
}

// UpdateAssetReq 更新资产请求
type UpdateAssetReq struct {
	AssetName  string    `json:"asset_name"`
	Status     string    `json:"status"`
	Tags       []Tag     `json:"tags"`
	Metadata   string    `json:"metadata"`
	Cost       float64   `json:"cost"`
	UpdateTime time.Time `json:"update_time"`
}

// ListAssetsReq 获取资产列表请求（已废弃，改用 query 参数）
// Deprecated: 使用 GET /api/v1/cam/assets?provider=xxx&offset=0&limit=20
type ListAssetsReq struct {
	Page
	Provider  string `json:"provider"`
	AssetType string `json:"asset_type"`
	Region    string `json:"region"`
	Status    string `json:"status"`
	AssetName string `json:"asset_name"`
}

// AssetListResp 资产列表响应
type AssetListResp struct {
	Assets []CloudAsset `json:"assets"`
	Total  int64        `json:"total"`
}

// DiscoverAssetsReq 发现资产请求
type DiscoverAssetsReq struct {
	Provider   string   `json:"provider" binding:"required"`
	Region     string   `json:"region"`
	AssetTypes []string `json:"asset_types"` // 要发现的资源类型，为空则发现所有支持的类型
}

// SyncAssetsReq 同步资产请求
// Deprecated: 请使用 POST /api/v1/cam/cloud-accounts/{id}/sync 接口
type SyncAssetsReq struct {
	AccountID  int64    `json:"account_id" binding:"required"` // 云账号ID
	AssetTypes []string `json:"asset_types"`                   // 要同步的资源类型，为空则同步所有支持的类型
}

// AssetStatisticsResp 资产统计响应
type AssetStatisticsResp struct {
	TotalAssets      int64            `json:"total_assets"`
	ProviderStats    map[string]int64 `json:"provider_stats"`
	AssetTypeStats   map[string]int64 `json:"asset_type_stats"`
	RegionStats      map[string]int64 `json:"region_stats"`
	StatusStats      map[string]int64 `json:"status_stats"`
	TotalCost        float64          `json:"total_cost"`
	LastDiscoverTime time.Time        `json:"last_discover_time"`
}

// CostAnalysisReq 成本分析请求（已废弃，改用 query 参数）
// Deprecated: 使用 GET /api/v1/cam/assets/cost-analysis?provider=xxx&days=30
type CostAnalysisReq struct {
	Provider string `json:"provider" binding:"required"`
	Days     int    `json:"days" binding:"min=1,max=365"`
}

// CostAnalysisResp 成本分析响应
type CostAnalysisResp struct {
	Provider    string             `json:"provider"`
	TotalCost   float64            `json:"total_cost"`
	DailyCosts  []DailyCost        `json:"daily_costs"`
	AssetCosts  []AssetCost        `json:"asset_costs"`
	RegionCosts map[string]float64 `json:"region_costs"`
}

// DailyCost 每日成本
type DailyCost struct {
	Date string  `json:"date"`
	Cost float64 `json:"cost"`
}

// AssetCost 资产成本
type AssetCost struct {
	AssetId   string  `json:"asset_id"`
	AssetName string  `json:"asset_name"`
	AssetType string  `json:"asset_type"`
	Cost      float64 `json:"cost"`
}

// ==================== 云账号相关 VO ====================

// CloudAccount 云账号VO
type CloudAccount struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	Provider        string               `json:"provider"`
	Environment     string               `json:"environment"`
	AccessKeyID     string               `json:"access_key_id"`
	AccessKeySecret string               `json:"access_key_secret,omitempty"` // 敏感信息，通常不返回
	Regions         []string             `json:"regions"`
	Description     string               `json:"description"`
	Status          string               `json:"status"`
	Config          CloudAccountConfigVO `json:"config"`
	TenantID        string               `json:"tenant_id"`
	LastSyncTime    *time.Time           `json:"last_sync_time"`
	LastTestTime    *time.Time           `json:"last_test_time"`
	AssetCount      int64                `json:"asset_count"`
	ErrorMessage    string               `json:"error_message"`
	CreateTime      time.Time            `json:"create_time"`
	UpdateTime      time.Time            `json:"update_time"`
}

// CloudAccountConfigVO 云账号配置VO
type CloudAccountConfigVO struct {
	EnableAutoSync       bool     `json:"enable_auto_sync"`
	SyncInterval         int64    `json:"sync_interval"`
	ReadOnly             bool     `json:"read_only"`
	ShowSubAccounts      bool     `json:"show_sub_accounts"`
	EnableCostMonitoring bool     `json:"enable_cost_monitoring"`
	SupportedRegions     []string `json:"supported_regions"`
	SupportedAssetTypes  []string `json:"supported_asset_types"`
}

// CreateCloudAccountReq 创建云账号请求
type CreateCloudAccountReq struct {
	Name            string               `json:"name" binding:"required,min=1,max=100"`
	Provider        string               `json:"provider" binding:"required"`
	Environment     string               `json:"environment" binding:"required"`
	AccessKeyID     string               `json:"access_key_id" binding:"required,min=16,max=128"`
	AccessKeySecret string               `json:"access_key_secret" binding:"required,min=16,max=256"`
	Regions         []string             `json:"regions" binding:"required,min=1"`
	Description     string               `json:"description" binding:"max=500"`
	Config          CloudAccountConfigVO `json:"config"`
	TenantID        string               `json:"tenant_id" binding:"required"`
}

// UpdateCloudAccountReq 更新云账号请求
type UpdateCloudAccountReq struct {
	Name            *string               `json:"name,omitempty"`
	Environment     *string               `json:"environment,omitempty"`
	AccessKeyID     *string               `json:"access_key_id,omitempty"`
	AccessKeySecret *string               `json:"access_key_secret,omitempty"`
	Regions         []string              `json:"regions,omitempty"`
	Description     *string               `json:"description,omitempty"`
	Config          *CloudAccountConfigVO `json:"config,omitempty"`
	TenantID        *string               `json:"tenant_id,omitempty"`
}

// ListCloudAccountsReq 获取云账号列表请求（已废弃，改用 query 参数）
// Deprecated: 使用 GET /api/v1/cam/cloud-accounts?provider=xxx&offset=0&limit=20
type ListCloudAccountsReq struct {
	Page
	Provider    string `json:"provider"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	TenantID    string `json:"tenant_id"`
}

// CloudAccountListResp 云账号列表响应
type CloudAccountListResp struct {
	Accounts []CloudAccount `json:"accounts"`
	Total    int64          `json:"total"`
}

// ConnectionTestResult 连接测试结果VO
type ConnectionTestResult struct {
	Status   string    `json:"status"`
	Message  string    `json:"message"`
	Regions  []string  `json:"regions"`
	TestTime time.Time `json:"test_time"`
}

// SyncAccountReq 同步账号资产请求
// @Description 同步云账号资产的请求参数
type SyncAccountReq struct {
	// 要同步的资源类型列表，支持: ecs, rds, oss, vpc 等
	// 为空则默认只同步 ecs
	AssetTypes []string `json:"asset_types" example:"ecs,rds"`
	// 要同步的地域列表，为空则同步账号配置的所有地域
	Regions []string `json:"regions" example:"cn-hangzhou,cn-shanghai"`
}

// SyncResult 同步结果VO
// @Description 云资产同步操作的结果
type SyncResult struct {
	// 同步任务ID，格式: sync_{account_id}_{timestamp}
	SyncID string `json:"sync_id" example:"sync_1_1706000000"`
	// 同步状态: running, success, failed
	Status string `json:"status" example:"success"`
	// 同步结果消息
	Message string `json:"message,omitempty" example:"同步完成，共同步 10 个资产"`
	// 同步开始时间
	StartTime time.Time `json:"start_time"`
}

// ==================== 模型管理相关 VO ====================

// ModelVO 模型VO
type ModelVO struct {
	ID           int64     `json:"id"`
	UID          string    `json:"uid"`
	Name         string    `json:"name"`
	ModelGroupID int64     `json:"model_group_id"`
	ParentUID    string    `json:"parent_uid,omitempty"`
	Category     string    `json:"category"`
	Level        int       `json:"level"`
	Icon         string    `json:"icon,omitempty"`
	Description  string    `json:"description,omitempty"`
	Provider     string    `json:"provider"`
	Extensible   bool      `json:"extensible"`
	CreateTime   time.Time `json:"create_time"`
	UpdateTime   time.Time `json:"update_time"`
}

// ModelFieldVO 模型字段VO
type ModelFieldVO struct {
	ID          int64     `json:"id"`
	FieldUID    string    `json:"field_uid"`
	FieldName   string    `json:"field_name"`
	FieldType   string    `json:"field_type"`
	ModelUID    string    `json:"model_uid"`
	GroupID     int64     `json:"group_id"`
	DisplayName string    `json:"display_name"` // 显示名称
	Display     bool      `json:"display"`      // 是否显示
	Index       int       `json:"index"`
	Required    bool      `json:"required"`
	Secure      bool      `json:"secure"`
	Link        bool      `json:"link"`                 // 是否为关联字段
	LinkModel   string    `json:"link_model,omitempty"` // 关联模型UID
	Option      string    `json:"option,omitempty"`
	CreateTime  time.Time `json:"create_time"`
	UpdateTime  time.Time `json:"update_time"`
}

// ModelFieldGroupVO 模型字段分组VO
type ModelFieldGroupVO struct {
	ID         int64     `json:"id"`
	ModelUID   string    `json:"model_uid"`
	Name       string    `json:"name"`
	Index      int       `json:"index"`
	CreateTime time.Time `json:"create_time"`
	UpdateTime time.Time `json:"update_time"`
}

// FieldGroupWithFieldsVO 带字段的分组VO
type FieldGroupWithFieldsVO struct {
	Group  *ModelFieldGroupVO `json:"group"`
	Fields []*ModelFieldVO    `json:"fields"`
}

// ModelDetailVO 模型详情VO
type ModelDetailVO struct {
	Model       *ModelVO                  `json:"model"`
	FieldGroups []*FieldGroupWithFieldsVO `json:"field_groups"`
}

// CreateModelReq 创建模型请求
type CreateModelReq struct {
	UID          string `json:"uid" binding:"required,min=1,max=100"`
	Name         string `json:"name" binding:"required,min=1,max=100"`
	ModelGroupID int64  `json:"model_group_id"`
	ParentUID    string `json:"parent_uid,omitempty"`
	Category     string `json:"category" binding:"required"`
	Level        int    `json:"level" binding:"required,min=1"`
	Icon         string `json:"icon,omitempty"`
	Description  string `json:"description,omitempty"`
	Provider     string `json:"provider" binding:"required"`
	Extensible   bool   `json:"extensible"`
}

// UpdateModelReq 更新模型请求
type UpdateModelReq struct {
	Name         string `json:"name,omitempty"`
	ModelGroupID int64  `json:"model_group_id,omitempty"`
	Icon         string `json:"icon,omitempty"`
	Description  string `json:"description,omitempty"`
	Extensible   bool   `json:"extensible"`
}

// ModelListResp 模型列表响应
type ModelListResp struct {
	Models []*ModelVO `json:"models"`
	Total  int64      `json:"total"`
}

// CreateFieldReq 创建字段请求
type CreateFieldReq struct {
	FieldUID    string `json:"field_uid" binding:"required,min=1,max=100"`
	FieldName   string `json:"field_name" binding:"required,min=1,max=100"`
	FieldType   string `json:"field_type" binding:"required"`
	ModelUID    string `json:"model_uid" binding:"required"`
	GroupID     int64  `json:"group_id"`
	DisplayName string `json:"display_name" binding:"required"` // 显示名称
	Display     bool   `json:"display"`                         // 是否显示（默认true）
	Index       int    `json:"index"`
	Required    bool   `json:"required"`
	Secure      bool   `json:"secure"`
	Link        bool   `json:"link"`                 // 是否为关联字段
	LinkModel   string `json:"link_model,omitempty"` // 关联模型UID
	Option      string `json:"option,omitempty"`
}

// UpdateFieldReq 更新字段请求
type UpdateFieldReq struct {
	FieldName   string `json:"field_name,omitempty"`
	FieldType   string `json:"field_type,omitempty"`
	GroupID     int64  `json:"group_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Display     bool   `json:"display"`
	Index       int    `json:"index,omitempty"`
	Required    bool   `json:"required"`
	Secure      bool   `json:"secure"`
	Link        bool   `json:"link"`                 // 是否为关联字段
	LinkModel   string `json:"link_model,omitempty"` // 关联模型UID
	Option      string `json:"option,omitempty"`
}

// FieldListResp 字段列表响应
type FieldListResp struct {
	Fields []*ModelFieldVO `json:"fields"`
	Total  int64           `json:"total"`
}

// CreateFieldGroupReq 创建字段分组请求
type CreateFieldGroupReq struct {
	ModelUID string `json:"model_uid" binding:"required"`
	Name     string `json:"name" binding:"required,min=1,max=100"`
	Index    int    `json:"index"`
}

// UpdateFieldGroupReq 更新字段分组请求
type UpdateFieldGroupReq struct {
	Name  string `json:"name,omitempty"`
	Index int    `json:"index,omitempty"`
}

// FieldGroupListResp 字段分组列表响应
type FieldGroupListResp struct {
	Groups []*ModelFieldGroupVO `json:"groups"`
	Total  int64                `json:"total"`
}

// ==================== 数据库资源相关 VO ====================

// RDSInstanceVO RDS实例VO
type RDSInstanceVO struct {
	InstanceID       string            `json:"instance_id"`
	InstanceName     string            `json:"instance_name"`
	Engine           string            `json:"engine"`
	EngineVersion    string            `json:"engine_version"`
	InstanceClass    string            `json:"instance_class"`
	InstanceStatus   string            `json:"instance_status"`
	ConnectionString string            `json:"connection_string"`
	Port             int               `json:"port"`
	VPCID            string            `json:"vpc_id"`
	VSwitchID        string            `json:"vswitch_id"`
	ZoneID           string            `json:"zone_id"`
	RegionID         string            `json:"region_id"`
	StorageType      string            `json:"storage_type"`
	StorageSize      int               `json:"storage_size"`
	MaxConnections   int               `json:"max_connections"`
	MaxIOPS          int               `json:"max_iops"`
	CreateTime       string            `json:"create_time"`
	ExpireTime       string            `json:"expire_time"`
	PayType          string            `json:"pay_type"`
	Tags             map[string]string `json:"tags"`
}

// RDSInstanceListResp RDS实例列表响应
type RDSInstanceListResp struct {
	Instances []RDSInstanceVO `json:"instances"`
	Total     int64           `json:"total"`
}

// RedisInstanceVO Redis实例VO
type RedisInstanceVO struct {
	InstanceID       string            `json:"instance_id"`
	InstanceName     string            `json:"instance_name"`
	InstanceClass    string            `json:"instance_class"`
	InstanceStatus   string            `json:"instance_status"`
	EngineVersion    string            `json:"engine_version"`
	Architecture     string            `json:"architecture"`
	NodeType         string            `json:"node_type"`
	ShardCount       int               `json:"shard_count"`
	ConnectionDomain string            `json:"connection_domain"`
	Port             int               `json:"port"`
	VPCID            string            `json:"vpc_id"`
	VSwitchID        string            `json:"vswitch_id"`
	ZoneID           string            `json:"zone_id"`
	RegionID         string            `json:"region_id"`
	Capacity         int64             `json:"capacity"`
	Bandwidth        int64             `json:"bandwidth"`
	Connections      int64             `json:"connections"`
	QPS              int64             `json:"qps"`
	CreateTime       string            `json:"create_time"`
	ExpireTime       string            `json:"expire_time"`
	PayType          string            `json:"pay_type"`
	Tags             map[string]string `json:"tags"`
}

// RedisInstanceListResp Redis实例列表响应
type RedisInstanceListResp struct {
	Instances []RedisInstanceVO `json:"instances"`
	Total     int64             `json:"total"`
}

// MongoDBInstanceVO MongoDB实例VO
type MongoDBInstanceVO struct {
	InstanceID       string            `json:"instance_id"`
	InstanceName     string            `json:"instance_name"`
	DBType           string            `json:"db_type"`
	EngineVersion    string            `json:"engine_version"`
	InstanceClass    string            `json:"instance_class"`
	InstanceStatus   string            `json:"instance_status"`
	StorageEngine    string            `json:"storage_engine"`
	ReplicaSetName   string            `json:"replica_set_name"`
	ConnectionString string            `json:"connection_string"`
	Port             int               `json:"port"`
	VPCID            string            `json:"vpc_id"`
	VSwitchID        string            `json:"vswitch_id"`
	ZoneID           string            `json:"zone_id"`
	RegionID         string            `json:"region_id"`
	StorageType      string            `json:"storage_type"`
	StorageSize      int               `json:"storage_size"`
	MaxConnections   int               `json:"max_connections"`
	MaxIOPS          int               `json:"max_iops"`
	CreateTime       string            `json:"create_time"`
	ExpireTime       string            `json:"expire_time"`
	PayType          string            `json:"pay_type"`
	Tags             map[string]string `json:"tags"`
}

// MongoDBInstanceListResp MongoDB实例列表响应
type MongoDBInstanceListResp struct {
	Instances []MongoDBInstanceVO `json:"instances"`
	Total     int64               `json:"total"`
}
