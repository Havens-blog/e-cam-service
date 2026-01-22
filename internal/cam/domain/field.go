package domain

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// ModelField 模型字段领域对象
type ModelField struct {
	ID          int64     // 业务ID
	FieldUID    string    // 字段唯一标识
	FieldName   string    // 字段名称
	FieldType   string    // 字段类型
	ModelUID    string    // 所属模型UID
	GroupID     int64     // 所属分组ID
	DisplayName string    // 显示名称（用于前端展示）
	Display     bool      // 是否显示（兼容旧系统）
	Index       int       // 排序索引
	Required    bool      // 是否必填
	Secure      bool      // 是否敏感字段
	Link        bool      // 是否为关联字段（兼容旧系统）
	LinkModel   string    // 关联模型UID
	Option      string    // 字段选项（JSON）
	CreateTime  time.Time // 创建时间
	UpdateTime  time.Time // 更新时间
}

// FieldType 字段类型常量
const (
	FieldTypeString   = "string"
	FieldTypeInt      = "int"
	FieldTypeFloat    = "float"
	FieldTypeBool     = "bool"
	FieldTypeDateTime = "datetime"
	FieldTypeArray    = "array"
	FieldTypeObject   = "object"
	FieldTypeEnum     = "enum"
	FieldTypeLink     = "link"
)

// ModelFieldFilter 字段过滤条件
type ModelFieldFilter struct {
	ModelUID  string
	GroupID   int64
	FieldType string
	Required  *bool
	Secure    *bool
	Offset    int
	Limit     int
}

// Validate 验证字段数据
func (f *ModelField) Validate() error {
	if f.FieldUID == "" {
		return errs.ErrInvalidFieldUID
	}
	if f.FieldName == "" {
		return errs.ErrInvalidFieldName
	}
	if f.ModelUID == "" {
		return errs.ErrInvalidModelUID
	}
	if !f.IsValidFieldType() {
		return errs.ErrInvalidFieldType
	}
	return nil
}

// IsValidFieldType 检查字段类型是否有效
func (f *ModelField) IsValidFieldType() bool {
	validTypes := []string{
		FieldTypeString, FieldTypeInt, FieldTypeFloat, FieldTypeBool,
		FieldTypeDateTime, FieldTypeArray, FieldTypeObject,
		FieldTypeEnum, FieldTypeLink,
	}
	for _, t := range validTypes {
		if f.FieldType == t {
			return true
		}
	}
	return false
}

// IsLinkField 是否为关联字段
func (f *ModelField) IsLinkField() bool {
	return f.FieldType == FieldTypeLink && f.LinkModel != ""
}
