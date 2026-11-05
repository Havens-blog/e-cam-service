package domain

// ChangeRecord 资产变更记录
type ChangeRecord struct {
	ID           int64  `json:"id" bson:"id"`
	AssetID      string `json:"asset_id" bson:"asset_id"`
	AssetName    string `json:"asset_name" bson:"asset_name"`
	ModelUID     string `json:"model_uid" bson:"model_uid"`
	TenantID     int64  `json:"tenant_id" bson:"tenant_id"`
	AccountID    int64  `json:"account_id" bson:"account_id"`
	Provider     string `json:"provider" bson:"provider"`
	Region       string `json:"region" bson:"region"`
	FieldName    string `json:"field_name" bson:"field_name"`
	OldValue     string `json:"old_value" bson:"old_value"` // JSON 序列化
	NewValue     string `json:"new_value" bson:"new_value"` // JSON 序列化
	ChangeSource string `json:"change_source" bson:"change_source"`
	ChangeTaskID string `json:"change_task_id" bson:"change_task_id"`
	// Operator 操作者（用户名）。e-cmdb-service 写入审计时从登录会话 claims
	// 的 username 提取；同步/导入等非交互操作可能为空。
	Operator string `json:"operator" bson:"operator,omitempty"`
	Ctime    int64  `json:"ctime" bson:"ctime"`
}

// ChangeFilter 变更记录查询过滤器
type ChangeFilter struct {
	AssetID   string `json:"asset_id"`
	TenantID  int64  `json:"tenant_id"`
	ModelUID  string `json:"model_uid"`
	Provider  string `json:"provider"`
	FieldName string `json:"field_name"`
	// ChangeSource 变更来源（manual=手工 / sync_task=云同步），精确匹配。
	// ecam_change_history 已有 change_source 单字段索引，等值过滤安全。
	ChangeSource string `json:"change_source"`
	StartTime    *int64 `json:"start_time"`
	EndTime      *int64 `json:"end_time"`
	Offset       int64  `json:"offset"`
	Limit        int64  `json:"limit"`
}

// ChangeSummary 变更统计汇总
type ChangeSummary struct {
	ByResourceType map[string]int64 `json:"by_resource_type"`
	ByField        map[string]int64 `json:"by_field"`
	ByProvider     map[string]int64 `json:"by_provider"`
	Total          int64            `json:"total"`
}

// ChangeMetadata 变更元数据，从同步上下文中提取
type ChangeMetadata struct {
	AssetID      string
	AssetName    string
	ModelUID     string
	TenantID     int64
	AccountID    int64
	Provider     string
	Region       string
	ChangeSource string
	ChangeTaskID string
}
