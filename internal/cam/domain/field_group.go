package domain

import (
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// ModelFieldGroup 模型字段分组领域对象
type ModelFieldGroup struct {
	ID         int64     // 业务ID
	ModelUID   string    // 所属模型UID
	Name       string    // 分组名称
	Index      int       // 排序索引
	CreateTime time.Time // 创建时间
	UpdateTime time.Time // 更新时间
}

// ModelFieldGroupFilter 分组过滤条件
type ModelFieldGroupFilter struct {
	ModelUID string
	Offset   int
	Limit    int
}

// Validate 验证分组数据
func (g *ModelFieldGroup) Validate() error {
	if g.ModelUID == "" {
		return errs.ErrInvalidModelUID
	}
	if g.Name == "" {
		return errs.ErrInvalidGroupName
	}
	return nil
}

// ModelDetail 模型详情（包含字段和分组）
type ModelDetail struct {
	Model       *Model
	FieldGroups []*FieldGroupWithFields
}

// FieldGroupWithFields 带字段的分组
type FieldGroupWithFields struct {
	Group  *ModelFieldGroup
	Fields []*ModelField
}
