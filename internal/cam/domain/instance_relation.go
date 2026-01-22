package domain

import "time"

// InstanceRelation 实例关系领域模型
type InstanceRelation struct {
	ID               int64  // 业务ID
	SourceInstanceID int64  // 源实例ID
	TargetInstanceID int64  // 目标实例ID
	RelationTypeUID  string // 关系类型UID
	TenantID         string // 租户ID
	CreateTime       time.Time
}

// InstanceRelationFilter 实例关系过滤条件
type InstanceRelationFilter struct {
	SourceInstanceID int64
	TargetInstanceID int64
	RelationTypeUID  string
	TenantID         string
	Offset           int64
	Limit            int64
}
