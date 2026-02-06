package domain

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
)

// ModelField 模型字段领域对象
type ModelField struct {
	ID          int64
	FieldUID    string
	FieldName   string
	FieldType   string
	ModelUID    string
	GroupID     int64
	DisplayName string
	Display     bool
	Index       int
	Required    bool
	Secure      bool
	Link        bool
	LinkModel   string
	Option      string
	CreateTime  time.Time
	UpdateTime  time.Time
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

// ModelFieldGroup 字段分组
type ModelFieldGroup struct {
	ID         int64
	ModelUID   string
	GroupName  string
	Index      int
	CreateTime time.Time
	UpdateTime time.Time
}

// ModelFieldGroupFilter 字段分组过滤条件
type ModelFieldGroupFilter struct {
	ModelUID string
	Offset   int
	Limit    int
}
