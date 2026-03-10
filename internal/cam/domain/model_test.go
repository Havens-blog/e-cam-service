package domain

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/stretchr/testify/assert"
)

func TestModel_Validate(t *testing.T) {
	tests := []struct {
		name    string
		model   Model
		wantErr error
	}{
		{
			name:    "有效模型",
			model:   Model{UID: "cloud_vm", Name: "云虚拟机", Category: "compute"},
			wantErr: nil,
		},
		{
			name:    "缺少UID",
			model:   Model{Name: "云虚拟机", Category: "compute"},
			wantErr: errs.ErrInvalidModelUID,
		},
		{
			name:    "缺少Name",
			model:   Model{UID: "cloud_vm", Category: "compute"},
			wantErr: errs.ErrInvalidModelName,
		},
		{
			name:    "缺少Category",
			model:   Model{UID: "cloud_vm", Name: "云虚拟机"},
			wantErr: errs.ErrInvalidModelCategory,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.model.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestModel_IsTopLevel(t *testing.T) {
	assert.True(t, (&Model{Level: 1, ParentUID: ""}).IsTopLevel())
	assert.False(t, (&Model{Level: 2, ParentUID: ""}).IsTopLevel())
	assert.False(t, (&Model{Level: 1, ParentUID: "parent"}).IsTopLevel())
	assert.False(t, (&Model{Level: 2, ParentUID: "parent"}).IsTopLevel())
}

func TestModel_IsSubModel(t *testing.T) {
	assert.True(t, (&Model{Level: 2, ParentUID: "parent"}).IsSubModel())
	assert.True(t, (&Model{Level: 3, ParentUID: "parent"}).IsSubModel())
	assert.False(t, (&Model{Level: 1, ParentUID: ""}).IsSubModel())
	assert.False(t, (&Model{Level: 2, ParentUID: ""}).IsSubModel())
	assert.False(t, (&Model{Level: 1, ParentUID: "parent"}).IsSubModel())
}
