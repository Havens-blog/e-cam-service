package domain

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/stretchr/testify/assert"
)

func TestModelField_Validate(t *testing.T) {
	tests := []struct {
		name    string
		field   ModelField
		wantErr error
	}{
		{
			name:    "有效字段",
			field:   ModelField{FieldUID: "instance_id", FieldName: "实例ID", ModelUID: "cloud_vm", FieldType: FieldTypeString},
			wantErr: nil,
		},
		{
			name:    "缺少FieldUID",
			field:   ModelField{FieldName: "实例ID", ModelUID: "cloud_vm", FieldType: FieldTypeString},
			wantErr: errs.ErrInvalidFieldUID,
		},
		{
			name:    "缺少FieldName",
			field:   ModelField{FieldUID: "instance_id", ModelUID: "cloud_vm", FieldType: FieldTypeString},
			wantErr: errs.ErrInvalidFieldName,
		},
		{
			name:    "缺少ModelUID",
			field:   ModelField{FieldUID: "instance_id", FieldName: "实例ID", FieldType: FieldTypeString},
			wantErr: errs.ErrInvalidModelUID,
		},
		{
			name:    "无效FieldType",
			field:   ModelField{FieldUID: "instance_id", FieldName: "实例ID", ModelUID: "cloud_vm", FieldType: "invalid_type"},
			wantErr: errs.ErrInvalidFieldType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.field.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestModelField_IsValidFieldType(t *testing.T) {
	validTypes := []string{
		FieldTypeString, FieldTypeInt, FieldTypeFloat, FieldTypeBool,
		FieldTypeDateTime, FieldTypeArray, FieldTypeObject,
		FieldTypeEnum, FieldTypeLink,
	}
	for _, ft := range validTypes {
		t.Run(ft, func(t *testing.T) {
			f := &ModelField{FieldType: ft}
			assert.True(t, f.IsValidFieldType())
		})
	}

	// 无效类型
	for _, ft := range []string{"", "invalid", "text", "number"} {
		t.Run("invalid_"+ft, func(t *testing.T) {
			f := &ModelField{FieldType: ft}
			assert.False(t, f.IsValidFieldType())
		})
	}
}

func TestModelField_IsLinkField(t *testing.T) {
	// 是关联字段
	f := &ModelField{FieldType: FieldTypeLink, LinkModel: "cloud_vpc"}
	assert.True(t, f.IsLinkField())

	// 类型是link但没有LinkModel
	f = &ModelField{FieldType: FieldTypeLink, LinkModel: ""}
	assert.False(t, f.IsLinkField())

	// 类型不是link
	f = &ModelField{FieldType: FieldTypeString, LinkModel: "cloud_vpc"}
	assert.False(t, f.IsLinkField())
}

func TestModelFieldGroup_Validate(t *testing.T) {
	tests := []struct {
		name    string
		group   ModelFieldGroup
		wantErr error
	}{
		{
			name:    "有效分组",
			group:   ModelFieldGroup{ModelUID: "cloud_vm", Name: "基本信息"},
			wantErr: nil,
		},
		{
			name:    "缺少ModelUID",
			group:   ModelFieldGroup{Name: "基本信息"},
			wantErr: errs.ErrInvalidModelUID,
		},
		{
			name:    "缺少Name",
			group:   ModelFieldGroup{ModelUID: "cloud_vm"},
			wantErr: errs.ErrInvalidGroupName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.group.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
