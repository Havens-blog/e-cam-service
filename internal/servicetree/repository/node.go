package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/repository/dao"
	"go.mongodb.org/mongo-driver/mongo"
)

// NodeRepository 服务树节点仓储接口
type NodeRepository interface {
	Create(ctx context.Context, node domain.ServiceTreeNode) (int64, error)
	Update(ctx context.Context, node domain.ServiceTreeNode) error
	UpdatePath(ctx context.Context, id int64, path string) error
	GetByID(ctx context.Context, id int64) (domain.ServiceTreeNode, error)
	GetByUID(ctx context.Context, tenantID, uid string) (domain.ServiceTreeNode, error)
	List(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, error)
	ListByPath(ctx context.Context, tenantID, pathPrefix string) ([]domain.ServiceTreeNode, error)
	Count(ctx context.Context, filter domain.NodeFilter) (int64, error)
	CountChildren(ctx context.Context, parentID int64) (int64, error)
	Delete(ctx context.Context, id int64) error
}

type nodeRepository struct {
	dao dao.NodeDAO
}

// NewNodeRepository 创建节点仓储
func NewNodeRepository(dao dao.NodeDAO) NodeRepository {
	return &nodeRepository{dao: dao}
}

func (r *nodeRepository) Create(ctx context.Context, node domain.ServiceTreeNode) (int64, error) {
	return r.dao.Create(ctx, r.toDAO(node))
}

func (r *nodeRepository) Update(ctx context.Context, node domain.ServiceTreeNode) error {
	return r.dao.Update(ctx, r.toDAO(node))
}

func (r *nodeRepository) UpdatePath(ctx context.Context, id int64, path string) error {
	return r.dao.UpdatePath(ctx, id, path)
}

func (r *nodeRepository) GetByID(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
	daoNode, err := r.dao.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.ServiceTreeNode{}, domain.ErrNodeNotFound
		}
		return domain.ServiceTreeNode{}, err
	}
	return r.toDomain(daoNode), nil
}

func (r *nodeRepository) GetByUID(ctx context.Context, tenantID, uid string) (domain.ServiceTreeNode, error) {
	daoNode, err := r.dao.GetByUID(ctx, tenantID, uid)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return domain.ServiceTreeNode{}, domain.ErrNodeNotFound
		}
		return domain.ServiceTreeNode{}, err
	}
	return r.toDomain(daoNode), nil
}

func (r *nodeRepository) List(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, error) {
	daoNodes, err := r.dao.List(ctx, r.toDAOFilter(filter))
	if err != nil {
		return nil, err
	}

	nodes := make([]domain.ServiceTreeNode, len(daoNodes))
	for i, daoNode := range daoNodes {
		nodes[i] = r.toDomain(daoNode)
	}
	return nodes, nil
}

func (r *nodeRepository) ListByPath(ctx context.Context, tenantID, pathPrefix string) ([]domain.ServiceTreeNode, error) {
	daoNodes, err := r.dao.ListByPath(ctx, tenantID, pathPrefix)
	if err != nil {
		return nil, err
	}

	nodes := make([]domain.ServiceTreeNode, len(daoNodes))
	for i, daoNode := range daoNodes {
		nodes[i] = r.toDomain(daoNode)
	}
	return nodes, nil
}

func (r *nodeRepository) Count(ctx context.Context, filter domain.NodeFilter) (int64, error) {
	return r.dao.Count(ctx, r.toDAOFilter(filter))
}

func (r *nodeRepository) CountChildren(ctx context.Context, parentID int64) (int64, error) {
	return r.dao.CountChildren(ctx, parentID)
}

func (r *nodeRepository) Delete(ctx context.Context, id int64) error {
	return r.dao.Delete(ctx, id)
}

func (r *nodeRepository) toDAO(node domain.ServiceTreeNode) dao.Node {
	return dao.Node{
		ID:          node.ID,
		UID:         node.UID,
		Name:        node.Name,
		ParentID:    node.ParentID,
		Level:       node.Level,
		Path:        node.Path,
		TenantID:    node.TenantID,
		Owner:       node.Owner,
		Team:        node.Team,
		Description: node.Description,
		Tags:        node.Tags,
		Order:       node.Order,
		Status:      node.Status,
	}
}

func (r *nodeRepository) toDomain(daoNode dao.Node) domain.ServiceTreeNode {
	return domain.ServiceTreeNode{
		ID:          daoNode.ID,
		UID:         daoNode.UID,
		Name:        daoNode.Name,
		ParentID:    daoNode.ParentID,
		Level:       daoNode.Level,
		Path:        daoNode.Path,
		TenantID:    daoNode.TenantID,
		Owner:       daoNode.Owner,
		Team:        daoNode.Team,
		Description: daoNode.Description,
		Tags:        daoNode.Tags,
		Order:       daoNode.Order,
		Status:      daoNode.Status,
		CreateTime:  time.UnixMilli(daoNode.Ctime),
		UpdateTime:  time.UnixMilli(daoNode.Utime),
	}
}

func (r *nodeRepository) toDAOFilter(filter domain.NodeFilter) dao.NodeFilter {
	return dao.NodeFilter{
		TenantID: filter.TenantID,
		ParentID: filter.ParentID,
		Level:    filter.Level,
		Status:   filter.Status,
		Name:     filter.Name,
		Owner:    filter.Owner,
		Team:     filter.Team,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}
}
