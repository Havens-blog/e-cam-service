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
	GetAssetNode(ctx context.Context, tenantID int64, resourceID int64) (domain.ServiceTreeNode, error)
	GetNodeAssetStats(ctx context.Context, tenantID int64, nodeID int64, includeChildren bool) (domain.AssetStats, error)
	GetGlobalAssetStats(ctx context.Context, tenantID int64) (domain.AssetStats, error)
	GetNodeAssetSummary(ctx context.Context, tenantID int64, nodeID int64) (domain.AssetSummary, error)
}

type nodeAssetService struct {
	bindingRepo repository.BindingRepository
	nodeRepo    repository.NodeRepository
	cmdbRepo    cmdbrepository.InstanceRepository
	envRepo     repository.EnvironmentRepository
	logger      *elog.Component
}

// NewNodeAssetService 创建节点资产查询服务
func NewNodeAssetService(
	bindingRepo repository.BindingRepository,
	nodeRepo repository.NodeRepository,
	cmdbRepo cmdbrepository.InstanceRepository,
	envRepo repository.EnvironmentRepository,
	logger *elog.Component,
) NodeAssetService {
	return &nodeAssetService{
		bindingRepo: bindingRepo,
		nodeRepo:    nodeRepo,
		cmdbRepo:    cmdbRepo,
		envRepo:     envRepo,
		logger:      logger,
	}
}

func (s *nodeAssetService) ListNodeAssets(ctx context.Context, filter domain.NodeAssetFilter) ([]domain.NodeAssetVO, int64, error) {
	// 先查节点，判断是否为根节点
	node, err := s.nodeRepo.GetByID(ctx, filter.NodeID)
	if err != nil {
		return nil, 0, fmt.Errorf("节点不存在: %w", err)
	}

	// 根节点特殊处理: 不查 binding 表，直接查未绑定的资产作为"待分配"资源池。
	// includeChildren 时与普通节点一致走子树聚合（getBindings 的 ListByPath 路径），
	// 口径与 GetNodeAssetSummary 统计卡对齐（仅子树内已绑定资产），否则统计卡与
	// 列表总数不一致（列表会混入全租户未绑定资产）。
	if node.IsRoot() && !filter.IncludeChildren {
		return s.listUnboundAssets(ctx, filter)
	}

	// type 过滤时取全量绑定：asset_type 派生自 model_uid，无法下推绑定表 DB 查询，
	// 只能在内存过滤后再分页，total 用过滤后的精确计数；否则走 DB 分页（env 过滤
	// 已在查询层生效，效率更高）。
	queryFilter := filter
	if filter.AssetType != "" {
		queryFilter.Offset = 0
		queryFilter.Limit = 0
	}

	// 1. 获取绑定列表
	bindings, total, err := s.getBindings(ctx, queryFilter)
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

	// 4. 组装结果，按 assetType 过滤（CMDB 缺失的绑定跳过）
	filtered := make([]domain.NodeAssetVO, 0, len(bindings))
	for _, b := range bindings {
		inst, ok := instanceMap[b.ResourceID]
		if !ok {
			s.logger.Warn("绑定的资源在CMDB中不存在",
				elog.Int64("bindingID", b.ID),
				elog.Int64("resourceID", b.ResourceID),
			)
			continue
		}

		assetType := extractAssetType(inst.ModelUID)
		if filter.AssetType != "" && assetType != filter.AssetType {
			continue
		}

		filtered = append(filtered, domain.NodeAssetVO{
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

	if filter.AssetType != "" {
		// type 过滤：内存分页 + 精确 total
		total = int64(len(filtered))
		filtered = slicePage(filtered, filter.Offset, filter.Limit)
	} else {
		// 无 type 过滤（DB 已分页）：total 来自 Count，剔除本页 CMDB 缺失的绑定
		total -= int64(len(bindings)) - int64(len(filtered))
	}

	return filtered, total, nil
}

// slicePage 内存分页（offset/limit 裁剪；limit<=0 表示不限制）
func slicePage(vos []domain.NodeAssetVO, offset, limit int64) []domain.NodeAssetVO {
	if offset < 0 {
		offset = 0
	}
	if offset >= int64(len(vos)) {
		return []domain.NodeAssetVO{}
	}
	start := int(offset)
	end := len(vos)
	if limit > 0 && int(offset+limit) < end {
		end = int(offset + limit)
	}
	return vos[start:end]
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

func (s *nodeAssetService) GetAssetNode(ctx context.Context, tenantID int64, resourceID int64) (domain.ServiceTreeNode, error) {
	binding, err := s.bindingRepo.GetByResource(ctx, tenantID, domain.ResourceTypeInstance, resourceID)
	if err != nil {
		return domain.ServiceTreeNode{}, err
	}
	return s.nodeRepo.GetByID(ctx, binding.NodeID)
}

func (s *nodeAssetService) GetNodeAssetStats(ctx context.Context, tenantID int64, nodeID int64, includeChildren bool) (domain.AssetStats, error) {
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
func (s *nodeAssetService) GetGlobalAssetStats(ctx context.Context, tenantID int64) (domain.AssetStats, error) {
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
	// 云厂商模型: 去掉厂商前缀段 "aliyun_security_group" → "security_group"。
	// 注意必须按首个下划线切,多词类型(security_group/elasticsearch)用
	// LastIndex 会切错成 "group"
	if idx := strings.Index(modelUID, "_"); idx > 0 {
		return modelUID[idx+1:]
	}
	return modelUID
}

// GetNodeAssetSummary 节点子树资产聚合统计（含自身），带环境错绑疑异检测。
// 聚合口径：先取子树节点 ID 集合（一次路径前缀查询），再对绑定表 node_id IN
// 一次取全部绑定（非逐节点递归），二段批量查实例明细后内存归并分布。
func (s *nodeAssetService) GetNodeAssetSummary(ctx context.Context, tenantID int64, nodeID int64) (domain.AssetSummary, error) {
	node, err := s.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return domain.AssetSummary{}, fmt.Errorf("节点不存在: %w", err)
	}

	// 1. 子树节点 ID 集合（含自身；ListByPath 前缀匹配含自身，此处防御性补齐）
	subNodes, err := s.nodeRepo.ListByPath(ctx, tenantID, node.Path)
	if err != nil {
		return domain.AssetSummary{}, fmt.Errorf("查询子树节点失败: %w", err)
	}
	nodeIDs := make([]int64, 0, len(subNodes)+1)
	seen := make(map[int64]bool, len(subNodes)+1)
	for _, n := range subNodes {
		if !seen[n.ID] {
			nodeIDs = append(nodeIDs, n.ID)
			seen[n.ID] = true
		}
	}
	if !seen[nodeID] {
		nodeIDs = append(nodeIDs, nodeID)
	}

	summary := domain.AssetSummary{
		ByEnvironment: make(map[string]int64),
		ByProvider:    make(map[string]int64),
		ByType:        make(map[string]int64),
		ByBindType:    make(map[string]int64),
		Suspicious:    make([]domain.AssetSuspicion, 0),
	}

	// 2. 绑定表 node_id IN 一次查全部绑定（不加 limit，聚合需全量）
	bindings, err := s.bindingRepo.ListByNodeIDs(ctx, domain.NodeIDsBindingFilter{
		TenantID:     tenantID,
		NodeIDs:      nodeIDs,
		ResourceType: domain.ResourceTypeInstance,
	})
	if err != nil {
		return domain.AssetSummary{}, fmt.Errorf("查询子树绑定失败: %w", err)
	}
	if len(bindings) == 0 {
		return summary, nil
	}

	// 3. 二段查：批量取实例明细（命名/tag.env/provider/model_uid）
	resourceIDs := make([]int64, 0, len(bindings))
	for _, b := range bindings {
		resourceIDs = append(resourceIDs, b.ResourceID)
	}
	instances, err := s.cmdbRepo.ListByIDs(ctx, resourceIDs)
	if err != nil {
		return domain.AssetSummary{}, fmt.Errorf("批量查询CMDB实例失败: %w", err)
	}
	instanceMap := make(map[int64]cmdbdomain.Instance, len(instances))
	for _, inst := range instances {
		instanceMap[inst.ID] = inst
	}

	// 4. 环境 ID → 代码映射（环境分布与错绑判定均按环境代码口径）
	envCodeByID, err := loadEnvCodeMap(ctx, s.envRepo, tenantID)
	if err != nil {
		return domain.AssetSummary{}, err
	}

	// 5. 内存归并聚合 + 疑异检测
	for _, b := range bindings {
		inst, ok := instanceMap[b.ResourceID]
		if !ok {
			s.logger.Warn("绑定的资源在CMDB中不存在",
				elog.Int64("bindingID", b.ID),
				elog.Int64("resourceID", b.ResourceID),
			)
			continue
		}

		summary.Total++
		summary.ByType[extractAssetType(inst.ModelUID)]++
		if provider := inst.GetStringAttribute("provider"); provider != "" {
			summary.ByProvider[provider]++
		}

		envKey, boundCode := resolveEnvKey(b.EnvID, envCodeByID)
		summary.ByEnvironment[envKey]++

		bindType := b.BindType
		if bindType == "" {
			bindType = domain.BindTypeManual
		}
		summary.ByBindType[bindType]++

		if reasons := detectEnvMismatch(inst.AssetName, extractTagEnv(inst), boundCode); len(reasons) > 0 {
			summary.Suspicious = append(summary.Suspicious, domain.AssetSuspicion{
				AssetID:      inst.AssetID,
				AssetName:    inst.AssetName,
				BoundEnvID:   b.EnvID,
				BoundEnvCode: boundCode,
				Reason:       strings.Join(reasons, "; "),
			})
		}
	}

	return summary, nil
}

// resolveEnvKey 环境分布 key：有代码用代码，未知环境回退 "env_<id>"
func resolveEnvKey(envID int64, envCodeByID map[int64]string) (key, code string) {
	code = envCodeByID[envID]
	if code != "" {
		return code, code
	}
	return fmt.Sprintf("env_%d", envID), ""
}

// 环境推断（normalizeEnvCode/matchNameEnvCode/extractTagEnv/detectEnvMismatch/
// loadEnvCodeMap 等）已收敛至 infer_env.go 单份公共实现，绑定/改绑/概览三方共用。

