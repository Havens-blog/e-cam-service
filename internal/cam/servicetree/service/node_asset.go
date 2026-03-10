package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	cmdbdomain "github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
	cmdbrepository "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	cmdbdao "github.com/Havens-blog/e-cam-service/internal/cmdb/repository/dao"
	"github.com/gotomicro/ego/core/elog"
)

// NodeAssetService 节点资产查询服务
type NodeAssetService interface {
	ListNodeAssets(ctx context.Context, filter domain.NodeAssetFilter) ([]domain.NodeAssetVO, int64, error)
	GetAssetNode(ctx context.Context, tenantID string, resourceID int64) (domain.ServiceTreeNode, error)
	GetNodeAssetStats(ctx context.Context, tenantID string, nodeID int64, includeChildren bool) (domain.AssetStats, error)
	GetGlobalAssetStats(ctx context.Context, tenantID string) (domain.AssetStats, error)
}

type nodeAssetService struct {
	bindingRepo repository.BindingRepository
	nodeRepo    repository.NodeRepository
	cmdbRepo    cmdbrepository.InstanceRepository
	logger      *elog.Component
}

// NewNodeAssetService 创建节点资产查询服务
func NewNodeAssetService(
	bindingRepo repository.BindingRepository,
	nodeRepo repository.NodeRepository,
	cmdbRepo cmdbrepository.InstanceRepository,
	logger *elog.Component,
) NodeAssetService {
	return &nodeAssetService{
		bindingRepo: bindingRepo,
		nodeRepo:    nodeRepo,
		cmdbRepo:    cmdbRepo,
		logger:      logger,
	}
}

func (s *nodeAssetService) ListNodeAssets(ctx context.Context, filter domain.NodeAssetFilter) ([]domain.NodeAssetVO, int64, error) {
	// 先查节点，判断是否为根节点
	node, err := s.nodeRepo.GetByID(ctx, filter.NodeID)
	if err != nil {
		return nil, 0, fmt.Errorf("节点不存在: %w", err)
	}

	// 根节点特殊处理: 不查 binding 表，直接查未绑定的资产作为"待分配"资源池
	if node.IsRoot() && !filter.IncludeChildren {
		return s.listUnboundAssets(ctx, filter)
	}
	// 根节点 + includeChildren: 返回该租户全部资产
	if node.IsRoot() && filter.IncludeChildren {
		return s.listAllAssets(ctx, filter)
	}

	// 1. 获取绑定列表
	bindings, total, err := s.getBindings(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if len(bindings) == 0 {
		return nil, 0, nil
	}

	// 2. 提取 ResourceID 列表，批量查 CMDB
	resourceIDs := make([]int64, len(bindings))
	for i, b := range bindings {
		resourceIDs[i] = b.ResourceID
	}
	instances, err := s.cmdbRepo.ListByIDs(ctx, resourceIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("批量查询CMDB实例失败: %w", err)
	}

	// 3. 构建 ID → Instance 映射
	instanceMap := make(map[int64]cmdbdomain.Instance, len(instances))
	for _, inst := range instances {
		instanceMap[inst.ID] = inst
	}

	// 4. 组装结果，按 assetType 过滤
	var result []domain.NodeAssetVO
	for _, b := range bindings {
		inst, ok := instanceMap[b.ResourceID]
		if !ok {
			s.logger.Warn("绑定的资源在CMDB中不存在",
				elog.Int64("bindingID", b.ID),
				elog.Int64("resourceID", b.ResourceID),
			)
			total--
			continue
		}

		assetType := extractAssetType(inst.ModelUID)
		if filter.AssetType != "" && assetType != filter.AssetType {
			total--
			continue
		}

		result = append(result, domain.NodeAssetVO{
			BindingID:  b.ID,
			NodeID:     b.NodeID,
			EnvID:      b.EnvID,
			BindType:   b.BindType,
			ID:         inst.ID,
			AssetID:    inst.AssetID,
			AssetName:  inst.AssetName,
			AssetType:  assetType,
			Provider:   inst.GetStringAttribute("provider"),
			Region:     inst.GetStringAttribute("region"),
			Status:     inst.GetStringAttribute("status"),
			AccountID:  inst.AccountID,
			Attributes: inst.Attributes,
			CreateTime: inst.CreateTime.UnixMilli(),
			UpdateTime: inst.UpdateTime.UnixMilli(),
		})
	}

	return result, total, nil
}

// listUnboundAssets 查询未绑定到任何节点的资产 (根节点的"待分配"资源池)
func (s *nodeAssetService) listUnboundAssets(ctx context.Context, filter domain.NodeAssetFilter) ([]domain.NodeAssetVO, int64, error) {
	instances, err := s.cmdbRepo.ListUnbound(ctx, filter.TenantID, filter.Offset, filter.Limit)
	if err != nil {
		return nil, 0, fmt.Errorf("查询未绑定资产失败: %w", err)
	}
	total, err := s.cmdbRepo.CountUnbound(ctx, filter.TenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("统计未绑定资产失败: %w", err)
	}

	return s.instancesToNodeAssetVOs(instances, filter, 0), total, nil
}

// listAllAssets 查询租户全部资产 (根节点 + includeChildren)
func (s *nodeAssetService) listAllAssets(ctx context.Context, filter domain.NodeAssetFilter) ([]domain.NodeAssetVO, int64, error) {
	cmdbFilter := cmdbdomain.InstanceFilter{
		TenantID: filter.TenantID,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}
	instances, err := s.cmdbRepo.List(ctx, cmdbFilter)
	if err != nil {
		return nil, 0, fmt.Errorf("查询全部资产失败: %w", err)
	}
	total, err := s.cmdbRepo.Count(ctx, cmdbFilter)
	if err != nil {
		return nil, 0, fmt.Errorf("统计全部资产失败: %w", err)
	}

	return s.instancesToNodeAssetVOs(instances, filter, 0), total, nil
}

// instancesToNodeAssetVOs 将 CMDB 实例列表转换为 NodeAssetVO (无 binding 信息)
func (s *nodeAssetService) instancesToNodeAssetVOs(instances []cmdbdomain.Instance, filter domain.NodeAssetFilter, nodeID int64) []domain.NodeAssetVO {
	var result []domain.NodeAssetVO
	for _, inst := range instances {
		assetType := extractAssetType(inst.ModelUID)
		if filter.AssetType != "" && assetType != filter.AssetType {
			continue
		}
		result = append(result, domain.NodeAssetVO{
			NodeID:     nodeID,
			ID:         inst.ID,
			AssetID:    inst.AssetID,
			AssetName:  inst.AssetName,
			AssetType:  assetType,
			Provider:   inst.GetStringAttribute("provider"),
			Region:     inst.GetStringAttribute("region"),
			Status:     inst.GetStringAttribute("status"),
			AccountID:  inst.AccountID,
			Attributes: inst.Attributes,
			CreateTime: inst.CreateTime.UnixMilli(),
			UpdateTime: inst.UpdateTime.UnixMilli(),
		})
	}
	return result
}

func (s *nodeAssetService) GetAssetNode(ctx context.Context, tenantID string, resourceID int64) (domain.ServiceTreeNode, error) {
	binding, err := s.bindingRepo.GetByResource(ctx, tenantID, domain.ResourceTypeInstance, resourceID)
	if err != nil {
		return domain.ServiceTreeNode{}, err
	}
	return s.nodeRepo.GetByID(ctx, binding.NodeID)
}

func (s *nodeAssetService) GetNodeAssetStats(ctx context.Context, tenantID string, nodeID int64, includeChildren bool) (domain.AssetStats, error) {
	// 查节点判断是否根节点
	node, err := s.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return domain.AssetStats{}, fmt.Errorf("节点不存在: %w", err)
	}

	var result *cmdbdao.AssetStatsResult

	if node.IsRoot() && !includeChildren {
		// 根节点: 聚合统计未绑定资产
		result, err = s.cmdbRepo.AggregateUnboundStats(ctx, tenantID)
		if err != nil {
			return domain.AssetStats{}, fmt.Errorf("统计未绑定资产失败: %w", err)
		}
	} else if node.IsRoot() && includeChildren {
		// 根节点 + 子节点: 聚合统计全部资产
		result, err = s.cmdbRepo.AggregateAllStats(ctx, tenantID)
		if err != nil {
			return domain.AssetStats{}, fmt.Errorf("统计全部资产失败: %w", err)
		}
	} else {
		// 普通节点: 先获取 binding 的 resource IDs，再聚合统计
		bindings, _, bindErr := s.getBindings(ctx, domain.NodeAssetFilter{
			TenantID:        tenantID,
			NodeID:          nodeID,
			IncludeChildren: includeChildren,
		})
		if bindErr != nil {
			return domain.AssetStats{}, bindErr
		}
		if len(bindings) == 0 {
			return domain.AssetStats{
				ByAssetType: make(map[string]int64),
				ByProvider:  make(map[string]int64),
			}, nil
		}

		resourceIDs := make([]int64, len(bindings))
		for i, b := range bindings {
			resourceIDs[i] = b.ResourceID
		}
		result, err = s.cmdbRepo.AggregateStatsByIDs(ctx, resourceIDs)
		if err != nil {
			return domain.AssetStats{}, fmt.Errorf("聚合统计资产失败: %w", err)
		}
	}

	// 转换结果
	stats := domain.AssetStats{
		Total:       result.Total,
		ByAssetType: make(map[string]int64),
		ByProvider:  make(map[string]int64),
	}
	for _, item := range result.ByAssetType {
		assetType := extractAssetType(item.AssetType)
		stats.ByAssetType[assetType] += item.Count
	}
	for _, item := range result.ByProvider {
		if item.Provider != "" {
			stats.ByProvider[item.Provider] = item.Count
		}
	}
	return stats, nil
}

// GetGlobalAssetStats 全局资产统计（不区分节点，按产品类别聚合）
func (s *nodeAssetService) GetGlobalAssetStats(ctx context.Context, tenantID string) (domain.AssetStats, error) {
	result, err := s.cmdbRepo.AggregateAllStats(ctx, tenantID)
	if err != nil {
		return domain.AssetStats{}, fmt.Errorf("统计全局资产失败: %w", err)
	}

	stats := domain.AssetStats{
		Total:       result.Total,
		ByAssetType: make(map[string]int64),
		ByProvider:  make(map[string]int64),
	}
	for _, item := range result.ByAssetType {
		assetType := extractAssetType(item.AssetType)
		stats.ByAssetType[assetType] += item.Count
	}
	for _, item := range result.ByProvider {
		if item.Provider != "" {
			stats.ByProvider[item.Provider] = item.Count
		}
	}
	return stats, nil
}

// getBindings 根据 filter 获取绑定列表
func (s *nodeAssetService) getBindings(ctx context.Context, filter domain.NodeAssetFilter) ([]domain.ResourceBinding, int64, error) {
	if !filter.IncludeChildren {
		// 单节点查询
		bf := domain.BindingFilter{
			TenantID:     filter.TenantID,
			NodeID:       filter.NodeID,
			EnvID:        filter.EnvID,
			ResourceType: domain.ResourceTypeInstance,
			Offset:       filter.Offset,
			Limit:        filter.Limit,
		}
		bindings, err := s.bindingRepo.List(ctx, bf)
		if err != nil {
			return nil, 0, err
		}
		total, err := s.bindingRepo.Count(ctx, bf)
		if err != nil {
			return nil, 0, err
		}
		return bindings, total, nil
	}

	// 包含子节点：先获取子树节点 ID
	node, err := s.nodeRepo.GetByID(ctx, filter.NodeID)
	if err != nil {
		return nil, 0, fmt.Errorf("节点不存在: %w", err)
	}

	childNodes, err := s.nodeRepo.ListByPath(ctx, filter.TenantID, node.Path)
	if err != nil {
		return nil, 0, fmt.Errorf("查询子节点失败: %w", err)
	}

	nodeIDs := make([]int64, len(childNodes))
	for i, n := range childNodes {
		nodeIDs[i] = n.ID
	}

	nf := domain.NodeIDsBindingFilter{
		TenantID:     filter.TenantID,
		NodeIDs:      nodeIDs,
		EnvID:        filter.EnvID,
		ResourceType: domain.ResourceTypeInstance,
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	}
	bindings, err := s.bindingRepo.ListByNodeIDs(ctx, nf)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.bindingRepo.CountByNodeIDs(ctx, nf)
	if err != nil {
		return nil, 0, err
	}
	return bindings, total, nil
}

// extractAssetType 从 model_uid 提取资产类型
// 例如 "aliyun_ecs" → "ecs", "cloud_vm" → "ecs", "rds" → "rds"
func extractAssetType(modelUID string) string {
	// 通用模型映射
	switch modelUID {
	case "cloud_vm":
		return "ecs"
	case "cloud_rds":
		return "rds"
	case "cloud_redis":
		return "redis"
	case "cloud_mongodb":
		return "mongodb"
	case "cloud_vpc":
		return "vpc"
	case "cloud_eip":
		return "eip"
	case "cloud_nas":
		return "nas"
	case "cloud_oss":
		return "oss"
	case "cloud_slb":
		return "slb"
	}
	// 云厂商模型: "aliyun_ecs" → "ecs"
	if idx := strings.LastIndex(modelUID, "_"); idx > 0 {
		return modelUID[idx+1:]
	}
	return modelUID
}
