package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// BindingService 资源绑定服务接口
type BindingService interface {
	BindResource(ctx context.Context, nodeID, envID int64, resourceType string, resourceID int64, tenantID string) (int64, error)
	BindResourceBatch(ctx context.Context, req domain.BatchBindRequest) (int64, error)
	UnbindResource(ctx context.Context, tenantID, resourceType string, resourceID int64) error
	UnbindByID(ctx context.Context, bindingID int64) error
	GetBinding(ctx context.Context, id int64) (domain.ResourceBinding, error)
	GetResourceBinding(ctx context.Context, tenantID, resourceType string, resourceID int64) (domain.ResourceBinding, error)
	ListBindings(ctx context.Context, filter domain.BindingFilter) ([]domain.ResourceBinding, int64, error)
	GetNodeResourceCount(ctx context.Context, nodeID int64) (int64, error)
	GetNodeEnvResourceCount(ctx context.Context, nodeID, envID int64) (int64, error)
}

type bindingService struct {
	bindingRepo repository.BindingRepository
	nodeRepo    repository.NodeRepository
	logger      *elog.Component
}

// NewBindingService 创建绑定服务
func NewBindingService(
	bindingRepo repository.BindingRepository,
	nodeRepo repository.NodeRepository,
	logger *elog.Component,
) BindingService {
	return &bindingService{
		bindingRepo: bindingRepo,
		nodeRepo:    nodeRepo,
		logger:      logger,
	}
}

func (s *bindingService) BindResource(ctx context.Context, nodeID, envID int64, resourceType string, resourceID int64, tenantID string) (int64, error) {
	if resourceType != domain.ResourceTypeInstance && resourceType != domain.ResourceTypeAsset {
		return 0, domain.ErrInvalidResourceType
	}

	_, err := s.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("节点不存在: %w", err)
	}

	existing, err := s.bindingRepo.GetByResource(ctx, tenantID, resourceType, resourceID)
	if err == nil {
		if existing.NodeID == nodeID && existing.EnvID == envID {
			return existing.ID, nil
		}
		return 0, domain.ErrBindingExists
	}
	if !errors.Is(err, domain.ErrBindingNotFound) {
		return 0, err
	}

	binding := domain.ResourceBinding{
		NodeID:       nodeID,
		EnvID:        envID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		TenantID:     tenantID,
		BindType:     domain.BindTypeManual,
	}

	id, err := s.bindingRepo.Create(ctx, binding)
	if err != nil {
		return 0, fmt.Errorf("创建绑定失败: %w", err)
	}

	s.logger.Info("绑定资源成功",
		elog.Int64("nodeID", nodeID),
		elog.Int64("envID", envID),
		elog.String("resourceType", resourceType),
		elog.Int64("resourceID", resourceID),
	)

	return id, nil
}

func (s *bindingService) BindResourceBatch(ctx context.Context, req domain.BatchBindRequest) (int64, error) {
	if len(req.ResourceIDs) == 0 {
		return 0, nil
	}

	_, err := s.nodeRepo.GetByID(ctx, req.NodeID)
	if err != nil {
		return 0, fmt.Errorf("节点不存在: %w", err)
	}

	var bindings []domain.ResourceBinding
	for _, resourceID := range req.ResourceIDs {
		_, err := s.bindingRepo.GetByResource(ctx, req.TenantID, req.ResourceType, resourceID)
		if err == nil {
			continue
		}
		if !errors.Is(err, domain.ErrBindingNotFound) {
			s.logger.Warn("检查绑定状态失败", elog.Int64("resourceID", resourceID), elog.FieldErr(err))
			continue
		}

		bindings = append(bindings, domain.ResourceBinding{
			NodeID:       req.NodeID,
			EnvID:        req.EnvID,
			ResourceType: req.ResourceType,
			ResourceID:   resourceID,
			TenantID:     req.TenantID,
			BindType:     domain.BindTypeManual,
		})
	}

	if len(bindings) == 0 {
		return 0, nil
	}

	count, err := s.bindingRepo.CreateBatch(ctx, bindings)
	if err != nil {
		return 0, fmt.Errorf("批量绑定失败: %w", err)
	}

	s.logger.Info("批量绑定资源成功",
		elog.Int64("nodeID", req.NodeID),
		elog.Int64("envID", req.EnvID),
		elog.Int64("count", count),
	)

	return count, nil
}

func (s *bindingService) UnbindResource(ctx context.Context, tenantID, resourceType string, resourceID int64) error {
	return s.bindingRepo.DeleteByResource(ctx, tenantID, resourceType, resourceID)
}

func (s *bindingService) UnbindByID(ctx context.Context, bindingID int64) error {
	return s.bindingRepo.Delete(ctx, bindingID)
}

func (s *bindingService) GetBinding(ctx context.Context, id int64) (domain.ResourceBinding, error) {
	return s.bindingRepo.GetByID(ctx, id)
}

func (s *bindingService) GetResourceBinding(ctx context.Context, tenantID, resourceType string, resourceID int64) (domain.ResourceBinding, error) {
	return s.bindingRepo.GetByResource(ctx, tenantID, resourceType, resourceID)
}

func (s *bindingService) ListBindings(ctx context.Context, filter domain.BindingFilter) ([]domain.ResourceBinding, int64, error) {
	bindings, err := s.bindingRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.bindingRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return bindings, total, nil
}

func (s *bindingService) GetNodeResourceCount(ctx context.Context, nodeID int64) (int64, error) {
	return s.bindingRepo.CountByNodeID(ctx, nodeID)
}

func (s *bindingService) GetNodeEnvResourceCount(ctx context.Context, nodeID, envID int64) (int64, error) {
	return s.bindingRepo.Count(ctx, domain.BindingFilter{NodeID: nodeID, EnvID: envID})
}
