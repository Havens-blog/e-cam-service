package domain

import (
	"fmt"
	"time"
)

// 关系类型常量
const (
	RelationBelongsTo = "belongs_to"
	RelationBindTo    = "bindto"
	RelationConnects  = "connects"
	RelationDependsOn = "depends_on"
	RelationRoute     = "route"
	RelationResolve   = "resolve"
	RelationCalls     = "calls"
)

// 流量方向常量
const (
	DirectionInbound       = "inbound"
	DirectionOutbound      = "outbound"
	DirectionBidirectional = "bidirectional"
)

// 边状态常量
const (
	EdgeStatusActive  = "active"
	EdgeStatusPending = "pending"
)

// ValidRelations 所有合法的关系类型
var ValidRelations = map[string]bool{
	RelationBelongsTo: true, RelationBindTo: true, RelationConnects: true,
	RelationDependsOn: true, RelationRoute: true, RelationResolve: true,
	RelationCalls: true,
}

// ValidDirections 所有合法的流量方向
var ValidDirections = map[string]bool{
	DirectionInbound: true, DirectionOutbound: true, DirectionBidirectional: true,
	"": true, // 允许为空
}

// TopoEdge 拓扑连线领域模型
type TopoEdge struct {
	ID              string                 `bson:"_id" json:"id"`
	SourceID        string                 `bson:"source_id" json:"source_id"`
	TargetID        string                 `bson:"target_id" json:"target_id"`
	Relation        string                 `bson:"relation" json:"relation"`
	Direction       string                 `bson:"direction" json:"direction"`
	SourceCollector string                 `bson:"source_collector" json:"source_collector"`
	Status          string                 `bson:"status" json:"status"`
	LastSeenAt      *time.Time             `bson:"last_seen_at,omitempty" json:"last_seen_at,omitempty"`
	RequestCount    *int64                 `bson:"request_count,omitempty" json:"request_count,omitempty"`
	LatencyP99      *float64               `bson:"latency_p99,omitempty" json:"latency_p99,omitempty"`
	Attributes      map[string]interface{} `bson:"attributes,omitempty" json:"attributes,omitempty"`
	TenantID        string                 `bson:"tenant_id" json:"tenant_id"`
	UpdatedAt       time.Time              `bson:"updated_at" json:"updated_at"`
}

// Validate 校验边字段合法性
func (e *TopoEdge) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("edge id is required")
	}
	if e.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}
	if e.TargetID == "" {
		return fmt.Errorf("target_id is required")
	}
	if !ValidRelations[e.Relation] {
		return fmt.Errorf("invalid relation: %s", e.Relation)
	}
	if !ValidDirections[e.Direction] {
		return fmt.Errorf("invalid direction: %s", e.Direction)
	}
	if e.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if e.Status == "" {
		e.Status = EdgeStatusActive
	}
	return nil
}

// IsPending 判断是否为悬挂边（目标节点尚未注册）
func (e *TopoEdge) IsPending() bool {
	return e.Status == EdgeStatusPending
}

// IsSilent 判断是否为沉默链路（超过 24 小时无流量）
func (e *TopoEdge) IsSilent(threshold time.Duration) bool {
	if e.LastSeenAt == nil {
		return false // 非日志来源的边不算沉默
	}
	return time.Since(*e.LastSeenAt) > threshold
}

// IsFromLog 判断是否来自日志采集
func (e *TopoEdge) IsFromLog() bool {
	return e.SourceCollector == SourceLog
}
