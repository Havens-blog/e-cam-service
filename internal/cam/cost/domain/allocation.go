package domain

// CostAllocation 成本分摊结果
type CostAllocation struct {
	ID              int64   `bson:"id" json:"id"`
	DimType         string  `bson:"dim_type" json:"dim_type"`
	DimValue        string  `bson:"dim_value" json:"dim_value"`
	DimPath         string  `bson:"dim_path" json:"dim_path"`
	NodeID          int64   `bson:"node_id" json:"node_id"`
	NodePath        string  `bson:"node_path" json:"node_path"`
	Period          string  `bson:"period" json:"period"`
	TotalAmount     float64 `bson:"total_amount" json:"total_amount"`
	DirectAmount    float64 `bson:"direct_amount" json:"direct_amount"`
	SharedAmount    float64 `bson:"shared_amount" json:"shared_amount"`
	RatioAmount     float64 `bson:"ratio_amount" json:"ratio_amount"`
	UnallocatedFlag bool    `bson:"unallocated_flag" json:"unallocated_flag"`
	DefaultFlag     bool    `bson:"default_flag" json:"default_flag"`
	RuleID          int64   `bson:"rule_id" json:"rule_id"`
	TenantID        string  `bson:"tenant_id" json:"tenant_id"`
	CreateTime      int64   `bson:"ctime" json:"ctime"`
}

// AllocationRule 分摊规则
type AllocationRule struct {
	ID              int64            `bson:"id" json:"id"`
	Name            string           `bson:"name" json:"name"`
	RuleType        string           `bson:"rule_type" json:"rule_type"`
	DimensionCombos []DimensionCombo `bson:"dimension_combos" json:"dimension_combos"`
	TagKey          string           `bson:"tag_key" json:"tag_key"`
	TagValueMap     map[string]int64 `bson:"tag_value_map" json:"tag_value_map"`
	SharedConfig    *SharedConfig    `bson:"shared_config" json:"shared_config"`
	Priority        int              `bson:"priority" json:"priority"`
	Status          string           `bson:"status" json:"status"`
	TenantID        string           `bson:"tenant_id" json:"tenant_id"`
	CreateTime      int64            `bson:"ctime" json:"ctime"`
	UpdateTime      int64            `bson:"utime" json:"utime"`
}

// DimensionCombo 维度组合（一条规则中的一个维度组合及其分摊比例）
type DimensionCombo struct {
	Dimensions []DimensionFilter `bson:"dimensions" json:"dimensions"`
	TargetID   string            `bson:"target_id" json:"target_id"`
	TargetName string            `bson:"target_name" json:"target_name"`
	Ratio      float64           `bson:"ratio" json:"ratio"`
}

// DimensionFilter 单个维度的筛选条件
type DimensionFilter struct {
	DimType  string `bson:"dim_type" json:"dim_type"`
	DimValue string `bson:"dim_value" json:"dim_value"`
}

// DefaultAllocationPolicy 默认分摊策略（兜底归属）
type DefaultAllocationPolicy struct {
	ID         int64  `bson:"id" json:"id"`
	TargetID   string `bson:"target_id" json:"target_id"`
	TargetName string `bson:"target_name" json:"target_name"`
	TenantID   string `bson:"tenant_id" json:"tenant_id"`
	CreateTime int64  `bson:"ctime" json:"ctime"`
	UpdateTime int64  `bson:"utime" json:"utime"`
}

// DimensionHierarchy 维度层级结构
type DimensionHierarchy struct {
	ID       int64  `bson:"id" json:"id"`
	DimType  string `bson:"dim_type" json:"dim_type"`
	NodeID   string `bson:"node_id" json:"node_id"`
	NodeName string `bson:"node_name" json:"node_name"`
	ParentID string `bson:"parent_id" json:"parent_id"`
	Path     string `bson:"path" json:"path"`
	TenantID string `bson:"tenant_id" json:"tenant_id"`
}

// SharedConfig 共享资源分摊配置
type SharedConfig struct {
	ResourceIDs []string          `bson:"resource_ids" json:"resource_ids"`
	Ratios      map[int64]float64 `bson:"ratios" json:"ratios"`
}
