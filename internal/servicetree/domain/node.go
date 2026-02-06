package domain

import (
	"fmt"
	"time"
)

// NodeLevel 节点层级常量
const (
	LevelBusinessLine = 1 // 业务线
	LevelProduct      = 2 // 产品
	LevelModule       = 3 // 模块
	LevelCluster      = 4 // 集群
)

// NodeStatus 节点状态
const (
	NodeStatusEnabled  = 1 // 启用
	NodeStatusDisabled = 0 // 禁用
)

// ServiceTreeNode 服务树节点领域模型
type ServiceTreeNode struct {
	ID          int64    // 节点ID
	UID         string   // 节点唯一标识 (如 biz.ecommerce.order)
	Name        string   // 节点名称
	ParentID    int64    // 父节点ID (0表示根节点)
	Level       int      // 层级 (1=业务线, 2=产品, 3=模块...)
	Path        string   // 完整路径 (如 /1/5/12/)，便于查询子树
	TenantID    string   // 租户ID
	Owner       string   // 负责人
	Team        string   // 所属团队
	Description string   // 描述
	Tags        []string // 标签
	Order       int      // 排序权重
	Status      int      // 状态 (1=启用, 0=禁用)
	CreateTime  time.Time
	UpdateTime  time.Time
}

// Validate 验证节点数据
func (n *ServiceTreeNode) Validate() error {
	if n.Name == "" {
		return fmt.Errorf("节点名称不能为空")
	}
	if n.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}
	return nil
}

// IsRoot 是否为根节点
func (n *ServiceTreeNode) IsRoot() bool {
	return n.ParentID == 0
}

// IsEnabled 是否启用
func (n *ServiceTreeNode) IsEnabled() bool {
	return n.Status == NodeStatusEnabled
}

// BuildPath 构建节点路径
func (n *ServiceTreeNode) BuildPath(parentPath string) string {
	if parentPath == "" {
		return fmt.Sprintf("/%d/", n.ID)
	}
	return fmt.Sprintf("%s%d/", parentPath, n.ID)
}

// NodeFilter 节点过滤条件
type NodeFilter struct {
	TenantID string // 租户ID
	ParentID *int64 // 父节点ID (nil表示不过滤)
	Level    int    // 层级
	Status   *int   // 状态
	Name     string // 名称模糊搜索
	Owner    string // 负责人
	Team     string // 团队
	Offset   int64
	Limit    int64
}

// NodeWithChildren 带子节点的树结构
type NodeWithChildren struct {
	ServiceTreeNode
	Children      []*NodeWithChildren `json:"children,omitempty"`
	ResourceCount int64               `json:"resource_count"` // 资源数量
}
