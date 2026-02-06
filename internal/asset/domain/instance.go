// Package domain 资产领域模型
package domain

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
)

// Instance 资产实例领域模型
type Instance struct {
	ID         int64
	ModelUID   string
	AssetID    string
	AssetName  string
	TenantID   string
	AccountID  int64
	Attributes map[string]interface{}
	CreateTime time.Time
	UpdateTime time.Time
}

// InstanceFilter 实例过滤条件
type InstanceFilter struct {
	ModelUID   string
	TenantID   string
	AccountID  int64
	AssetID    string
	AssetName  string
	Provider   string
	TagFilter  *TagFilter
	Attributes map[string]interface{}
	Offset     int64
	Limit      int64
}

// TagFilter 标签过滤条件
type TagFilter struct {
	HasTags bool
	NoTags  bool
	Key     string
	Value   string
}

// SearchFilter 统一搜索过滤条件
type SearchFilter struct {
	TenantID   string
	Keyword    string
	AssetTypes []string
	Provider   string
	AccountID  int64
	Region     string
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
