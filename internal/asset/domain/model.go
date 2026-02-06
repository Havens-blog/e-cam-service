package domain

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
)

// Model 资源模型领域对象
type Model struct {
	ID           int64
	UID          string
	Name         string
	ModelGroupID int64
	ParentUID    string
	Category     string
	Level        int
	Icon         string
	Description  string
	Provider     string
	Extensible   bool
	CreateTime   time.Time
	UpdateTime   time.Time
}

// ModelFilter 模型过滤条件
type ModelFilter struct {
	Provider   string
	Category   string
	ParentUID  string
	Level      int
	Extensible *bool
	Offset     int
	Limit      int
}

// Validate 验证模型数据
func (m *Model) Validate() error {
	if m.UID == "" {
		return errs.ErrInvalidModelUID
	}
	if m.Name == "" {
		return errs.ErrInvalidModelName
	}
	if m.Category == "" {
		return errs.ErrInvalidModelCategory
	}
	return nil
}

// IsTopLevel 是否为顶级模型
func (m *Model) IsTopLevel() bool {
	return m.Level == 1 && m.ParentUID == ""
}

// IsSubModel 是否为子模型
func (m *Model) IsSubModel() bool {
	return m.Level > 1 && m.ParentUID != ""
}
