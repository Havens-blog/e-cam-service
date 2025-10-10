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
	Id         int64     `json:"id" binding:"required"`
	AssetName  string    `json:"asset_name"`
	Status     string    `json:"status"`
	Tags       []Tag     `json:"tags"`
	Metadata   string    `json:"metadata"`
	Cost       float64   `json:"cost"`
	UpdateTime time.Time `json:"update_time"`
}

// ListAssetsReq 获取资产列表请求
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
	Provider string `json:"provider" binding:"required"`
	Region   string `json:"region"`
}

// SyncAssetsReq 同步资产请求
type SyncAssetsReq struct {
	Provider string `json:"provider" binding:"required"`
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

// CostAnalysisReq 成本分析请求
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
	Region          string               `json:"region"`
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
	Region          string               `json:"region" binding:"required"`
	Description     string               `json:"description" binding:"max=500"`
	Config          CloudAccountConfigVO `json:"config"`
	TenantID        string               `json:"tenant_id" binding:"required"`
}

// UpdateCloudAccountReq 更新云账号请求
type UpdateCloudAccountReq struct {
	Name        *string               `json:"name,omitempty"`
	Description *string               `json:"description,omitempty"`
	Config      *CloudAccountConfigVO `json:"config,omitempty"`
}

// ListCloudAccountsReq 获取云账号列表请求
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

// SyncAccountReq 同步账号请求
type SyncAccountReq struct {
	AssetTypes []string `json:"asset_types"`
	Regions    []string `json:"regions"`
}

// SyncResult 同步结果VO
type SyncResult struct {
	SyncID    string    `json:"sync_id"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
}
