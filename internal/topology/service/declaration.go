package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository"
)

// DeclarationService 声明式拓扑注册服务接口
type DeclarationService interface {
	// Register 注册声明数据（校验 + 转换 + Upsert + pending 边激活）
	Register(ctx context.Context, decl domain.LinkDeclaration) error
	// DeleteBySource 按上报方标识批量删除声明数据及其对应的节点和边
	DeleteBySource(ctx context.Context, tenantID, source string) (int64, error)
	// List 查询租户下所有声明数据
	List(ctx context.Context, tenantID string) ([]domain.LinkDeclaration, error)
}

type declarationService struct {
	declRepo repository.DeclarationRepository
	nodeRepo repository.NodeRepository
	edgeRepo repository.EdgeRepository
}

// NewDeclarationService 创建声明服务
func NewDeclarationService(
	declRepo repository.DeclarationRepository,
	nodeRepo repository.NodeRepository,
	edgeRepo repository.EdgeRepository,
) DeclarationService {
	return &declarationService{
		declRepo: declRepo,
		nodeRepo: nodeRepo,
		edgeRepo: edgeRepo,
	}
}

// Register 注册声明数据
func (s *declarationService) Register(ctx context.Context, decl domain.LinkDeclaration) error {
	// 1. 校验声明数据
	if err := decl.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// 2. 保存原始声明数据
	if err := s.declRepo.Upsert(ctx, decl); err != nil {
		return fmt.Errorf("failed to save declaration: %w", err)
	}

	// 3. 转换为拓扑节点并 Upsert
	node := decl.ToTopoNode()

	// APM 声明的节点保护：如果节点已存在且来源不是 apm，则跳过节点更新，仅处理边
	skipNodeUpsert := false
	if decl.Source == "arms-apm" {
		existingNode, err := s.nodeRepo.FindByID(ctx, node.ID)
		if err == nil && existingNode.ID != "" && existingNode.SourceCollector != domain.SourceAPM {
			skipNodeUpsert = true
		}
	}

	if !skipNodeUpsert {
		if err := s.nodeRepo.Upsert(ctx, node); err != nil {
			return fmt.Errorf("failed to upsert node: %w", err)
		}
	}

	// 4. 转换为拓扑边
	edges := decl.ToTopoEdges()

	// 5. 检查每条边的目标节点是否存在，不存在则标记为 pending
	for i := range edges {
		targetNode, err := s.nodeRepo.FindByID(ctx, edges[i].TargetID)
		if err != nil {
			return fmt.Errorf("failed to check target node: %w", err)
		}
		if targetNode.ID == "" {
			edges[i].Status = domain.EdgeStatusPending
		}
	}

	// 6. Upsert 边
	if err := s.edgeRepo.UpsertMany(ctx, edges); err != nil {
		return fmt.Errorf("failed to upsert edges: %w", err)
	}

	// 7. 检查是否有 pending 边可以被当前新注册的节点激活
	activated, err := s.edgeRepo.UpdatePendingEdges(ctx, decl.TenantID, node.ID)
	if err != nil {
		return fmt.Errorf("failed to activate pending edges: %w", err)
	}
	if activated > 0 {
		// 有边被激活，说明之前有声明引用了这个节点但当时节点不存在
		_ = activated // 可以记录日志
	}

	return nil
}

// DeleteBySource 按上报方标识批量删除
func (s *declarationService) DeleteBySource(ctx context.Context, tenantID, source string) (int64, error) {
	// 1. 查询该 source 的所有声明，获取节点 ID 列表
	decls, err := s.declRepo.FindBySource(ctx, tenantID, source)
	if err != nil {
		return 0, fmt.Errorf("failed to find declarations: %w", err)
	}

	// 2. 删除对应的节点和边
	for _, d := range decls {
		_ = s.nodeRepo.Delete(ctx, d.Node.ID)
		_, _ = s.edgeRepo.DeleteByNodeID(ctx, tenantID, d.Node.ID)
	}

	// 3. 删除声明数据
	count, err := s.declRepo.DeleteBySource(ctx, tenantID, source)
	if err != nil {
		return 0, fmt.Errorf("failed to delete declarations: %w", err)
	}

	return count, nil
}

// List 查询所有声明数据
func (s *declarationService) List(ctx context.Context, tenantID string) ([]domain.LinkDeclaration, error) {
	return s.declRepo.FindAll(ctx, tenantID)
}
