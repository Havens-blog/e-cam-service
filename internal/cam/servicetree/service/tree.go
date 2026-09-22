package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// TreeService 服务树服务接口
type TreeService interface {
	// 节点管理
	CreateNode(ctx context.Context, node domain.ServiceTreeNode) (int64, error)
	UpdateNode(ctx context.Context, node domain.ServiceTreeNode) error
	DeleteNode(ctx context.Context, id int64) error
	GetNode(ctx context.Context, id int64) (domain.ServiceTreeNode, error)
	GetNodeByUID(ctx context.Context, tenantID int64, uid string) (domain.ServiceTreeNode, error)
	MoveNode(ctx context.Context, nodeID, newParentID int64) error

	// 树查询
	ListNodes(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, int64, error)
	GetTree(ctx context.Context, tenantID int64, rootID int64) (*domain.NodeWithChildren, error)
	GetSubTree(ctx context.Context, tenantID int64, nodeID int64) ([]domain.ServiceTreeNode, error)
	GetAncestors(ctx context.Context, nodeID int64) ([]domain.ServiceTreeNode, error)

	// RebuildPaths 按 parent_id 自顶向下重算全租户节点路径，修复历史脏 path。
	// 返回修复的节点数。幂等：path 已正确则跳过。
	RebuildPaths(ctx context.Context, tenantID int64) (int, error)
}

type treeService struct {
	nodeRepo    repository.NodeRepository
	bindingRepo repository.BindingRepository
	logger      *elog.Component
}

// NewTreeService 创建服务树服务
func NewTreeService(
	nodeRepo repository.NodeRepository,
	bindingRepo repository.BindingRepository,
	logger *elog.Component,
) TreeService {
	return &treeService{
		nodeRepo:    nodeRepo,
		bindingRepo: bindingRepo,
		logger:      logger,
	}
}

func (s *treeService) CreateNode(ctx context.Context, node domain.ServiceTreeNode) (int64, error) {
	// 验证节点数据
	if err := node.Validate(); err != nil {
		return 0, err
	}

	// 检查 UID 是否已存在
	if node.UID != "" {
		_, err := s.nodeRepo.GetByUID(ctx, node.TenantID, node.UID)
		if err == nil {
			return 0, domain.ErrNodeUIDExists
		}
		if err != domain.ErrNodeNotFound {
			return 0, err
		}
	}

	// 处理父节点
	var parentPath string
	if node.ParentID > 0 {
		parent, err := s.nodeRepo.GetByID(ctx, node.ParentID)
		if err != nil {
			return 0, domain.ErrNodeParentInvalid
		}
		parentPath = parent.Path
		node.Level = parent.Level + 1
	} else {
		node.Level = domain.LevelBusinessLine
	}

	// 设置默认状态
	if node.Status == 0 {
		node.Status = domain.NodeStatusEnabled
	}

	// 创建节点
	id, err := s.nodeRepo.Create(ctx, node)
	if err != nil {
		return 0, fmt.Errorf("创建节点失败: %w", err)
	}

	// 更新节点路径。Create 不回写 node.ID，必须先回填真实 ID 再 BuildPath，
	// 否则路径中节点 ID 恒为 0，兄弟节点 path 相同，子树按路径前缀聚合会塌缩。
	node.ID = id
	path := node.BuildPath(parentPath)
	if err := s.nodeRepo.UpdatePath(ctx, id, path); err != nil {
		s.logger.Error("更新节点路径失败", elog.Int64("nodeID", id), elog.FieldErr(err))
	}

	s.logger.Info("创建服务树节点成功", elog.Int64("nodeID", id), elog.String("name", node.Name))
	return id, nil
}

func (s *treeService) UpdateNode(ctx context.Context, node domain.ServiceTreeNode) error {
	// 获取原节点
	existing, err := s.nodeRepo.GetByID(ctx, node.ID)
	if err != nil {
		return err
	}

	// 检查 UID 唯一性
	if node.UID != "" && node.UID != existing.UID {
		_, err := s.nodeRepo.GetByUID(ctx, node.TenantID, node.UID)
		if err == nil {
			return domain.ErrNodeUIDExists
		}
		if err != domain.ErrNodeNotFound {
			return err
		}
	}

	// 保留不可变字段
	node.TenantID = existing.TenantID
	node.ParentID = existing.ParentID
	node.Level = existing.Level
	node.Path = existing.Path

	return s.nodeRepo.Update(ctx, node)
}

func (s *treeService) DeleteNode(ctx context.Context, id int64) error {
	// 检查是否有子节点
	childCount, err := s.nodeRepo.CountChildren(ctx, id)
	if err != nil {
		return err
	}
	if childCount > 0 {
		return domain.ErrNodeHasChildren
	}

	// 检查是否有绑定资源
	bindingCount, err := s.bindingRepo.CountByNodeID(ctx, id)
	if err != nil {
		return err
	}
	if bindingCount > 0 {
		return domain.ErrNodeHasBindings
	}

	return s.nodeRepo.Delete(ctx, id)
}

func (s *treeService) GetNode(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
	return s.nodeRepo.GetByID(ctx, id)
}

func (s *treeService) GetNodeByUID(ctx context.Context, tenantID int64, uid string) (domain.ServiceTreeNode, error) {
	return s.nodeRepo.GetByUID(ctx, tenantID, uid)
}

func (s *treeService) MoveNode(ctx context.Context, nodeID, newParentID int64) error {
	// 获取当前节点
	node, err := s.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return err
	}

	// 不能移动到自己
	if nodeID == newParentID {
		return domain.ErrNodeCyclicRef
	}

	var newParentPath string
	var newLevel int

	if newParentID > 0 {
		// 获取新父节点
		newParent, err := s.nodeRepo.GetByID(ctx, newParentID)
		if err != nil {
			return domain.ErrNodeParentInvalid
		}

		// 检查是否移动到子节点下 (循环引用)
		if strings.HasPrefix(newParent.Path, node.Path) {
			return domain.ErrNodeCyclicRef
		}

		newParentPath = newParent.Path
		newLevel = newParent.Level + 1
	} else {
		newLevel = domain.LevelBusinessLine
	}

	// 获取所有子节点
	subNodes, err := s.nodeRepo.ListByPath(ctx, node.TenantID, node.Path)
	if err != nil {
		return err
	}

	oldPath := node.Path
	newPath := fmt.Sprintf("%s%d/", newParentPath, nodeID)
	if newParentPath == "" {
		newPath = fmt.Sprintf("/%d/", nodeID)
	}

	// 更新当前节点
	node.ParentID = newParentID
	node.Level = newLevel
	node.Path = newPath
	if err := s.nodeRepo.Update(ctx, node); err != nil {
		return err
	}

	// 更新所有子节点的路径和层级
	levelDiff := newLevel - (node.Level - 1)
	for _, subNode := range subNodes {
		if subNode.ID == nodeID {
			continue
		}
		subNode.Path = strings.Replace(subNode.Path, oldPath, newPath, 1)
		subNode.Level += levelDiff
		if err := s.nodeRepo.Update(ctx, subNode); err != nil {
			s.logger.Error("更新子节点失败", elog.Int64("nodeID", subNode.ID), elog.FieldErr(err))
		}
	}

	return nil
}

func (s *treeService) ListNodes(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, int64, error) {
	nodes, err := s.nodeRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.nodeRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return nodes, total, nil
}

func (s *treeService) GetTree(ctx context.Context, tenantID int64, rootID int64) (*domain.NodeWithChildren, error) {
	var nodes []domain.ServiceTreeNode
	var err error

	if rootID > 0 {
		// 获取指定节点及其子树
		root, err := s.nodeRepo.GetByID(ctx, rootID)
		if err != nil {
			return nil, err
		}
		subNodes, err := s.nodeRepo.ListByPath(ctx, tenantID, root.Path)
		if err != nil {
			return nil, err
		}
		nodes = append([]domain.ServiceTreeNode{root}, subNodes...)
	} else {
		// 获取所有节点
		nodes, err = s.nodeRepo.List(ctx, domain.NodeFilter{TenantID: tenantID})
		if err != nil {
			return nil, err
		}
	}

	// 构建树结构
	return s.buildTree(nodes, rootID), nil
}

// RebuildPaths 按 parent_id 自顶向下重算全租户节点路径，修复历史脏 path
// （CreateNode 曾用未回填的 node.ID 拼路径，导致兄弟节点 path 相同、子树按
// 路径前缀聚合塌缩——见 CreateNode 修复注释）。幂等：path 已正确则跳过。
// 返回实际修复的节点数。
func (s *treeService) RebuildPaths(ctx context.Context, tenantID int64) (int, error) {
	nodes, err := s.nodeRepo.List(ctx, domain.NodeFilter{TenantID: tenantID})
	if err != nil {
		return 0, fmt.Errorf("查询节点列表失败: %w", err)
	}

	// 按 level 升序（父先于子），确保计算子路径时父路径已就绪
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Level < nodes[j].Level })

	pathByID := make(map[int64]string, len(nodes))
	fixed := 0
	for _, node := range nodes {
		var want string
		if node.ParentID > 0 {
			want = fmt.Sprintf("%s%d/", pathByID[node.ParentID], node.ID) // 父缺失时 parentPath 为空 → /id/
		} else {
			want = fmt.Sprintf("/%d/", node.ID)
		}
		pathByID[node.ID] = want
		if node.Path != want {
			if err := s.nodeRepo.UpdatePath(ctx, node.ID, want); err != nil {
				s.logger.Error("重建节点路径失败", elog.Int64("nodeID", node.ID), elog.FieldErr(err))
				continue
			}
			fixed++
		}
	}
	return fixed, nil
}

func (s *treeService) GetSubTree(ctx context.Context, tenantID int64, nodeID int64) ([]domain.ServiceTreeNode, error) {
	node, err := s.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return s.nodeRepo.ListByPath(ctx, tenantID, node.Path)
}

func (s *treeService) GetAncestors(ctx context.Context, nodeID int64) ([]domain.ServiceTreeNode, error) {
	node, err := s.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// 解析路径获取祖先节点ID
	parts := strings.Split(strings.Trim(node.Path, "/"), "/")
	ancestors := make([]domain.ServiceTreeNode, 0, len(parts)-1)

	for _, part := range parts {
		if part == "" {
			continue
		}
		var id int64
		fmt.Sscanf(part, "%d", &id)
		if id == nodeID {
			continue
		}
		ancestor, err := s.nodeRepo.GetByID(ctx, id)
		if err != nil {
			continue
		}
		ancestors = append(ancestors, ancestor)
	}

	return ancestors, nil
}

// buildTree 构建树结构
func (s *treeService) buildTree(nodes []domain.ServiceTreeNode, rootID int64) *domain.NodeWithChildren {
	if len(nodes) == 0 {
		return nil
	}

	// 构建节点映射
	nodeMap := make(map[int64]*domain.NodeWithChildren)
	for _, n := range nodes {
		nodeMap[n.ID] = &domain.NodeWithChildren{
			ServiceTreeNode: n,
			Children:        make([]*domain.NodeWithChildren, 0),
		}
	}

	// 构建父子关系
	var root *domain.NodeWithChildren
	for _, n := range nodes {
		node := nodeMap[n.ID]
		if n.ID == rootID || (rootID == 0 && n.ParentID == 0) {
			root = node
		} else if parent, ok := nodeMap[n.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	// 如果没有找到根节点，返回虚拟根
	if root == nil && rootID == 0 {
		root = &domain.NodeWithChildren{
			Children: make([]*domain.NodeWithChildren, 0),
		}
		for _, n := range nodes {
			if n.ParentID == 0 {
				root.Children = append(root.Children, nodeMap[n.ID])
			}
		}
	}

	return root
}
