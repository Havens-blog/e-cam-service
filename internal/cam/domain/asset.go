package domain

import "time"

// CloudAsset 云资产领域模型
type CloudAsset struct {
	Id           int64     // 资产ID
	AssetId      string    // 云厂商资产ID
	AssetName    string    // 资产名称
	AssetType    string    // 资产类型 (ecs, rds, oss, etc.)
	Provider     string    // 云厂商 (aliyun, aws, azure)
	Region       string    // 地域
	Zone         string    // 可用区
	Status       string    // 资产状态
	Tags         []Tag     // 标签
	Metadata     string    // 元数据 (JSON格式)
	Cost         float64   // 成本
	CreateTime   time.Time // 创建时间
	UpdateTime   time.Time // 更新时间
	DiscoverTime time.Time // 发现时间
}

// Tag 资产标签
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AssetFilter 资产过滤条件
type AssetFilter struct {
	Provider  string
	AssetType string
	Region    string
	Status    string
	AssetName string
	Offset    int64
	Limit     int64
}
