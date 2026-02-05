package domain

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// Instance 资产实例领域模型
type Instance struct {
	ID         int64                  // 业务ID
	ModelUID   string                 // 关联的模型UID (如 aliyun_ecs)
	AssetID    string                 // 云厂商资产ID (如 i-bp1234xxx)
	AssetName  string                 // 资产名称
	TenantID   string                 // 租户ID
	AccountID  int64                  // 云账号ID
	Attributes map[string]interface{} // 动态属性
	CreateTime time.Time
	UpdateTime time.Time
}

// InstanceFilter 实例过滤条件
type InstanceFilter struct {
	ModelUID   string                 // 按模型过滤
	TenantID   string                 // 按租户过滤
	AccountID  int64                  // 按云账号过滤
	AssetID    string                 // 按资产ID精确过滤
	AssetName  string                 // 按名称模糊搜索
	Provider   string                 // 按云平台过滤 (aliyun/aws/huawei/tencent/volcengine)
	TagFilter  *TagFilter             // 标签过滤条件
	Attributes map[string]interface{} // 按属性过滤
	Offset     int64
	Limit      int64
}

// TagFilter 标签过滤条件
type TagFilter struct {
	HasTags bool   // 过滤有标签的实例
	NoTags  bool   // 过滤没有标签的实例
	Key     string // 标签键
	Value   string // 标签值 (需配合Key使用)
}

// SearchFilter 统一搜索过滤条件
type SearchFilter struct {
	TenantID   string   // 租户ID (必填)
	Keyword    string   // 搜索关键词 (匹配 asset_id, asset_name, ip 等)
	AssetTypes []string // 资产类型列表 (ecs, rds, redis, mongodb, vpc, eip)，为空则搜索所有类型
	Provider   string   // 云厂商过滤
	AccountID  int64    // 云账号过滤
	Region     string   // 地域过滤
	Offset     int64
	Limit      int64
}

// SearchResult 搜索结果
type SearchResult struct {
	Items []Instance `json:"items"`
	Total int64      `json:"total"`
}

// Validate 验证实例数据
func (i *Instance) Validate() error {
	if i.ModelUID == "" {
		return errs.ErrInvalidModelUID
	}
	if i.AssetID == "" {
		return errs.ErrInvalidAssetID
	}
	if i.TenantID == "" {
		return errs.ErrInvalidTenantID
	}
	return nil
}

// GetAttribute 获取指定属性值
func (i *Instance) GetAttribute(key string) (interface{}, bool) {
	if i.Attributes == nil {
		return nil, false
	}
	val, ok := i.Attributes[key]
	return val, ok
}

// SetAttribute 设置属性值
func (i *Instance) SetAttribute(key string, value interface{}) {
	if i.Attributes == nil {
		i.Attributes = make(map[string]interface{})
	}
	i.Attributes[key] = value
}

// GetStringAttribute 获取字符串类型属性
func (i *Instance) GetStringAttribute(key string) string {
	val, ok := i.GetAttribute(key)
	if !ok {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// GetIntAttribute 获取整数类型属性
func (i *Instance) GetIntAttribute(key string) int64 {
	val, ok := i.GetAttribute(key)
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}
