package web

import "github.com/Havens-blog/e-cam-service/internal/servicetree/domain"

// CreateNodeReq 创建节点请求
type CreateNodeReq struct {
	UID         string   `json:"uid"`                     // 节点唯一标识
	Name        string   `json:"name" binding:"required"` // 节点名称
	ParentID    int64    `json:"parent_id"`               // 父节点ID
	Owner       string   `json:"owner"`                   // 负责人
	Team        string   `json:"team"`                    // 团队
	Description string   `json:"description"`             // 描述
	Tags        []string `json:"tags"`                    // 标签
	Order       int      `json:"order"`                   // 排序
}

// UpdateNodeReq 更新节点请求
type UpdateNodeReq struct {
	UID         string   `json:"uid"`
	Name        string   `json:"name" binding:"required"`
	Owner       string   `json:"owner"`
	Team        string   `json:"team"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Order       int      `json:"order"`
	Status      int      `json:"status"`
}

// MoveNodeReq 移动节点请求
type MoveNodeReq struct {
	NewParentID int64 `json:"new_parent_id"`
}

// NodeVO 节点响应
type NodeVO struct {
	ID          int64    `json:"id"`
	UID         string   `json:"uid"`
	Name        string   `json:"name"`
	ParentID    int64    `json:"parent_id"`
	Level       int      `json:"level"`
	Path        string   `json:"path"`
	Owner       string   `json:"owner"`
	Team        string   `json:"team"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Order       int      `json:"order"`
	Status      int      `json:"status"`
	CreateTime  int64    `json:"create_time"`
	UpdateTime  int64    `json:"update_time"`
}

// TreeNodeVO 树节点响应 (带子节点)
type TreeNodeVO struct {
	NodeVO
	Children      []*TreeNodeVO `json:"children,omitempty"`
	ResourceCount int64         `json:"resource_count"`
}

// BindResourceReq 绑定资源请求
type BindResourceReq struct {
	EnvID        int64  `json:"env_id" binding:"required"`        // 环境ID
	ResourceType string `json:"resource_type" binding:"required"` // instance/asset
	ResourceID   int64  `json:"resource_id" binding:"required"`
}

// BatchBindReq 批量绑定请求
type BatchBindReq struct {
	EnvID        int64   `json:"env_id" binding:"required"`
	ResourceType string  `json:"resource_type" binding:"required"`
	ResourceIDs  []int64 `json:"resource_ids" binding:"required"`
}

// BindingVO 绑定响应
type BindingVO struct {
	ID           int64  `json:"id"`
	NodeID       int64  `json:"node_id"`
	EnvID        int64  `json:"env_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   int64  `json:"resource_id"`
	BindType     string `json:"bind_type"`
	RuleID       int64  `json:"rule_id,omitempty"`
	CreateTime   int64  `json:"create_time"`
}

// CreateRuleReq 创建规则请求
type CreateRuleReq struct {
	NodeID      int64                  `json:"node_id" binding:"required"`
	EnvID       int64                  `json:"env_id" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Priority    int                    `json:"priority"`
	Conditions  []domain.RuleCondition `json:"conditions" binding:"required"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description"`
}

// UpdateRuleReq 更新规则请求
type UpdateRuleReq struct {
	NodeID      int64                  `json:"node_id" binding:"required"`
	EnvID       int64                  `json:"env_id" binding:"required"`
	Name        string                 `json:"name" binding:"required"`
	Priority    int                    `json:"priority"`
	Conditions  []domain.RuleCondition `json:"conditions" binding:"required"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description"`
}

// RuleVO 规则响应
type RuleVO struct {
	ID          int64                  `json:"id"`
	NodeID      int64                  `json:"node_id"`
	EnvID       int64                  `json:"env_id"`
	Name        string                 `json:"name"`
	Priority    int                    `json:"priority"`
	Conditions  []domain.RuleCondition `json:"conditions"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description"`
	CreateTime  int64                  `json:"create_time"`
	UpdateTime  int64                  `json:"update_time"`
}

// ListNodeReq 节点列表请求
type ListNodeReq struct {
	ParentID *int64 `form:"parent_id"`
	Level    int    `form:"level"`
	Status   *int   `form:"status"`
	Name     string `form:"name"`
	Owner    string `form:"owner"`
	Team     string `form:"team"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// ListBindingReq 绑定列表请求
type ListBindingReq struct {
	NodeID       int64  `form:"node_id"`
	EnvID        int64  `form:"env_id"`
	ResourceType string `form:"resource_type"`
	BindType     string `form:"bind_type"`
	Page         int    `form:"page"`
	PageSize     int    `form:"page_size"`
}

// ListRuleReq 规则列表请求
type ListRuleReq struct {
	NodeID   int64  `form:"node_id"`
	Enabled  *bool  `form:"enabled"`
	Name     string `form:"name"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// EnvironmentVO 环境响应
type EnvironmentVO struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Order       int    `json:"order"`
	Status      int    `json:"status"`
	CreateTime  int64  `json:"create_time"`
	UpdateTime  int64  `json:"update_time"`
}

// CreateEnvReq 创建环境请求
type CreateEnvReq struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Order       int    `json:"order"`
}

// UpdateEnvReq 更新环境请求
type UpdateEnvReq struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Order       int    `json:"order"`
	Status      int    `json:"status"`
}

// ListEnvReq 环境列表请求
type ListEnvReq struct {
	Code     string `form:"code"`
	Status   *int   `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}
