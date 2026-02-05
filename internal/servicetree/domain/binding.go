package domain

import "time"

// ResourceType 资源类型
const (
	ResourceTypeInstance = "instance" // 资产实例
	ResourceTypeAsset    = "asset"    // 云资产
)

// BindType 绑定方式
const (
	BindTypeManual = "manual" // 手动绑定
	BindTypeRule   = "rule"   // 规则匹配
)

// ResourceBinding 资源绑定关系
type ResourceBinding struct {
	ID           int64  // 绑定ID
	NodeID       int64  // 服务树节点ID
	EnvID        int64  // 环境ID
	ResourceType string // 资源类型 (instance/asset)
	ResourceID   int64  // 资源ID
	TenantID     string // 租户ID
	BindType     string // 绑定方式 (manual/rule)
	RuleID       int64  // 规则ID (当 BindType=rule 时有效)
	CreateTime   time.Time
}

// BindingFilter 绑定过滤条件
type BindingFilter struct {
	TenantID     string
	NodeID       int64
	EnvID        int64
	ResourceType string
	ResourceID   int64
	BindType     string
	Offset       int64
	Limit        int64
}

// BatchBindRequest 批量绑定请求
type BatchBindRequest struct {
	NodeID       int64   // 目标节点ID
	EnvID        int64   // 环境ID
	ResourceType string  // 资源类型
	ResourceIDs  []int64 // 资源ID列表
	TenantID     string  // 租户ID
}

// ResourceWithNode 带节点信息的资源
type ResourceWithNode struct {
	ResourceType string           // 资源类型
	ResourceID   int64            // 资源ID
	Node         *ServiceTreeNode // 所属节点
	BindType     string           // 绑定方式
}
