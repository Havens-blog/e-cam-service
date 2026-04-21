package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository"
)

// TopologyService 拓扑服务接口
type TopologyService interface {
	// GetBusinessTopology 获取业务链路拓扑（DAG 模式）
	GetBusinessTopology(ctx context.Context, params domain.TopologyQueryParams) (*domain.TopoGraph, error)
	// GetInstanceTopology 获取实例归属拓扑（树模式）
	GetInstanceTopology(ctx context.Context, params domain.TopologyQueryParams) (*domain.TopoGraph, error)
	// GetNodeDetail 获取单个节点详情（含上下游关系）
	GetNodeDetail(ctx context.Context, tenantID, nodeID string) (*domain.NodeDetail, error)
	// GetDomains 获取所有 DNS 入口域名列表
	GetDomains(ctx context.Context, tenantID string) ([]domain.DomainItem, error)
	// GetStats 获取拓扑统计信息
	GetStats(ctx context.Context, tenantID string) (*domain.TopoStats, error)
}

type topologyService struct {
	nodeRepo    repository.NodeRepository
	edgeRepo    repository.EdgeRepository
	builder     *DagBuilder
	liveBuilder *LiveTopologyBuilder
}

// NewTopologyService 创建拓扑服务
func NewTopologyService(
	nodeRepo repository.NodeRepository,
	edgeRepo repository.EdgeRepository,
	liveBuilder *LiveTopologyBuilder,
) TopologyService {
	return &topologyService{
		nodeRepo:    nodeRepo,
		edgeRepo:    edgeRepo,
		builder:     NewDagBuilder(),
		liveBuilder: liveBuilder,
	}
}

// GetBusinessTopology 获取业务链路拓扑
func (s *topologyService) GetBusinessTopology(ctx context.Context, params domain.TopologyQueryParams) (*domain.TopoGraph, error) {
	// 1. 构建节点过滤条件
	nodeFilter := domain.NodeFilter{TenantID: params.TenantID}
	if params.Provider != "" {
		nodeFilter.Providers = strings.Split(params.Provider, ",")
	}
	if params.Region != "" {
		nodeFilter.Regions = strings.Split(params.Region, ",")
	}
	if params.Type != "" {
		nodeFilter.Types = strings.Split(params.Type, ",")
	}
	if params.SourceCollector != "" {
		nodeFilter.SourceCollectors = strings.Split(params.SourceCollector, ",")
	}

	// 2. 查询节点
	nodes, err := s.nodeRepo.Find(ctx, nodeFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	if len(nodes) == 0 {
		// topo_nodes 为空，fallback 到从 DNS 记录实时构建
		if s.liveBuilder != nil {
			return s.liveBuilder.BuildFromDNS(ctx, params.TenantID, params.Domain)
		}
		return &domain.TopoGraph{
			Nodes: []domain.TopoNode{},
			Edges: []domain.TopoEdge{},
			Stats: domain.TopoStats{},
		}, nil
	}

	// 3. 收集节点 ID 用于查询边
	nodeIDs := make([]string, len(nodes))
	nodeMap := make(map[string]bool, len(nodes))
	for i, n := range nodes {
		nodeIDs[i] = n.ID
		nodeMap[n.ID] = true
	}

	// 4. 查询边（source 或 target 在节点集合中的边）
	edgeFilter := domain.EdgeFilter{
		TenantID:   params.TenantID,
		HideSilent: params.HideSilent,
	}
	if params.SourceCollector != "" {
		edgeFilter.SourceCollectors = strings.Split(params.SourceCollector, ",")
	}
	allEdges, err := s.edgeRepo.Find(ctx, edgeFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}

	// 5. 过滤边：只保留两端都在节点集合中的边（或 pending 边保留源端在集合中的）
	edges := make([]domain.TopoEdge, 0, len(allEdges))
	for _, e := range allEdges {
		if nodeMap[e.SourceID] && (nodeMap[e.TargetID] || e.Status == domain.EdgeStatusPending) {
			edges = append(edges, e)
		}
	}

	// 6. 计算 DAG 深度
	s.builder.ComputeDepths(nodes, edges)

	// 7. 按域名筛选子图
	if params.Domain != "" {
		nodes, edges = s.filterByDomain(nodes, edges, params.Domain)
	}

	// 8. 计算统计信息
	stats := s.computeStats(nodes, edges)

	return &domain.TopoGraph{
		Nodes: nodes,
		Edges: edges,
		Stats: stats,
	}, nil
}

// GetInstanceTopology 获取实例归属拓扑
func (s *topologyService) GetInstanceTopology(ctx context.Context, params domain.TopologyQueryParams) (*domain.TopoGraph, error) {
	if params.ResourceID == "" {
		return nil, fmt.Errorf("resource_id is required for instance mode")
	}

	// 1. 获取中心节点
	centerNode, err := s.nodeRepo.FindByID(ctx, params.ResourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node: %w", err)
	}
	if centerNode.ID == "" {
		return &domain.TopoGraph{
			Nodes: []domain.TopoNode{},
			Edges: []domain.TopoEdge{},
		}, nil
	}

	// 2. 获取与中心节点相关的所有边
	relatedEdges, err := s.edgeRepo.FindByNodeID(ctx, params.TenantID, params.ResourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to find related edges: %w", err)
	}

	// 3. 收集关联节点 ID
	relatedIDs := make(map[string]bool)
	relatedIDs[params.ResourceID] = true
	for _, e := range relatedEdges {
		relatedIDs[e.SourceID] = true
		relatedIDs[e.TargetID] = true
	}

	// 4. 查询关联节点
	ids := make([]string, 0, len(relatedIDs))
	for id := range relatedIDs {
		ids = append(ids, id)
	}
	nodes, err := s.nodeRepo.FindByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to find related nodes: %w", err)
	}

	return &domain.TopoGraph{
		Nodes: nodes,
		Edges: relatedEdges,
		Stats: domain.TopoStats{
			NodeCount: len(nodes),
			EdgeCount: len(relatedEdges),
		},
	}, nil
}

// GetNodeDetail 获取单个节点详情
func (s *topologyService) GetNodeDetail(ctx context.Context, tenantID, nodeID string) (*domain.NodeDetail, error) {
	node, err := s.nodeRepo.FindByID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node: %w", err)
	}

	// 如果 topo_nodes 中找不到，尝试从 live builder 的最近一次构建结果中查找
	if node.ID == "" && s.liveBuilder != nil {
		return s.getNodeDetailFromLive(ctx, tenantID, nodeID)
	}

	if node.ID == "" {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	// 查询入边（上游）
	inEdges, err := s.edgeRepo.FindByTargetID(ctx, tenantID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find upstream edges: %w", err)
	}

	// 查询出边（下游）
	outEdges, err := s.edgeRepo.FindBySourceID(ctx, tenantID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find downstream edges: %w", err)
	}

	// 收集上下游节点 ID
	upIDs := make([]string, 0, len(inEdges))
	for _, e := range inEdges {
		upIDs = append(upIDs, e.SourceID)
	}
	downIDs := make([]string, 0, len(outEdges))
	for _, e := range outEdges {
		downIDs = append(downIDs, e.TargetID)
	}

	upNodes, _ := s.nodeRepo.FindByIDs(ctx, upIDs)
	downNodes, _ := s.nodeRepo.FindByIDs(ctx, downIDs)

	return &domain.NodeDetail{
		TopoNode:        node,
		UpstreamNodes:   upNodes,
		DownstreamNodes: downNodes,
		UpstreamEdges:   inEdges,
		DownstreamEdges: outEdges,
	}, nil
}

// getNodeDetailFromLive 从 live builder 实时构建拓扑并查找节点详情
func (s *topologyService) getNodeDetailFromLive(ctx context.Context, tenantID, nodeID string) (*domain.NodeDetail, error) {
	// 从 nodeID 推断域名过滤条件
	domainFilter := ""
	if strings.HasPrefix(nodeID, "dns-") {
		domainFilter = strings.TrimPrefix(nodeID, "dns-")
	}

	graph, err := s.liveBuilder.BuildFromDNS(ctx, tenantID, domainFilter)
	if err != nil {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	// 在构建的图中查找目标节点
	var targetNode *domain.TopoNode
	nodeMap := make(map[string]*domain.TopoNode)
	for i := range graph.Nodes {
		nodeMap[graph.Nodes[i].ID] = &graph.Nodes[i]
		if graph.Nodes[i].ID == nodeID {
			targetNode = &graph.Nodes[i]
		}
	}

	if targetNode == nil {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	// 从图中提取上下游关系
	var upNodes, downNodes []domain.TopoNode
	var upEdges, downEdges []domain.TopoEdge
	for _, e := range graph.Edges {
		if e.TargetID == nodeID {
			upEdges = append(upEdges, e)
			if n := nodeMap[e.SourceID]; n != nil {
				upNodes = append(upNodes, *n)
			}
		}
		if e.SourceID == nodeID {
			downEdges = append(downEdges, e)
			if n := nodeMap[e.TargetID]; n != nil {
				downNodes = append(downNodes, *n)
			}
		}
	}

	return &domain.NodeDetail{
		TopoNode:        *targetNode,
		UpstreamNodes:   upNodes,
		DownstreamNodes: downNodes,
		UpstreamEdges:   upEdges,
		DownstreamEdges: downEdges,
	}, nil
}

// GetDomains 获取所有 DNS 入口域名列表
func (s *topologyService) GetDomains(ctx context.Context, tenantID string) ([]domain.DomainItem, error) {
	dnsNodes, err := s.nodeRepo.FindDNSEntries(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to find DNS entries: %w", err)
	}

	items := make([]domain.DomainItem, 0, len(dnsNodes))
	for _, n := range dnsNodes {
		items = append(items, domain.DomainItem{
			Domain:   n.Name,
			Provider: n.Provider,
			NodeID:   n.ID,
		})
	}
	return items, nil
}

// GetStats 获取拓扑统计信息
func (s *topologyService) GetStats(ctx context.Context, tenantID string) (*domain.TopoStats, error) {
	nodeCount, err := s.nodeRepo.Count(ctx, domain.NodeFilter{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	edgeCount, err := s.edgeRepo.Count(ctx, domain.EdgeFilter{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	dnsNodes, err := s.nodeRepo.FindDNSEntries(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	pendingCount, err := s.edgeRepo.CountPending(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return &domain.TopoStats{
		NodeCount:   int(nodeCount),
		EdgeCount:   int(edgeCount),
		DomainCount: len(dnsNodes),
		BrokenCount: int(pendingCount), // 简化：pending 边数作为断链数
	}, nil
}

// filterByDomain 按域名筛选子图：从指定 DNS 节点出发，BFS 找到所有可达节点和边
func (s *topologyService) filterByDomain(nodes []domain.TopoNode, edges []domain.TopoEdge, domainName string) ([]domain.TopoNode, []domain.TopoEdge) {
	// 找到域名对应的 DNS 入口节点
	var entryID string
	for _, n := range nodes {
		if n.Type == domain.NodeTypeDNSRecord && n.Name == domainName {
			entryID = n.ID
			break
		}
	}
	if entryID == "" {
		return nil, nil
	}

	// 构建邻接表
	adj := make(map[string][]string)
	edgeMap := make(map[string]domain.TopoEdge)
	for _, e := range edges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		edgeMap[e.ID] = e
	}

	// BFS 从入口节点出发
	visited := make(map[string]bool)
	queue := []string{entryID}
	visited[entryID] = true
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, next := range adj[curr] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	// 过滤节点和边
	filteredNodes := make([]domain.TopoNode, 0)
	for _, n := range nodes {
		if visited[n.ID] {
			filteredNodes = append(filteredNodes, n)
		}
	}
	filteredEdges := make([]domain.TopoEdge, 0)
	for _, e := range edges {
		if visited[e.SourceID] && visited[e.TargetID] {
			filteredEdges = append(filteredEdges, e)
		}
	}

	return filteredNodes, filteredEdges
}

// computeStats 计算统计信息
func (s *topologyService) computeStats(nodes []domain.TopoNode, edges []domain.TopoEdge) domain.TopoStats {
	stats := domain.TopoStats{
		NodeCount: len(nodes),
		EdgeCount: len(edges),
	}

	for _, n := range nodes {
		if n.Type == domain.NodeTypeDNSRecord {
			stats.DomainCount++
		}
		if n.DagDepth > stats.MaxDepth {
			stats.MaxDepth = n.DagDepth
		}
	}

	// 断链检测
	stats.BrokenCount = s.builder.DetectBrokenLinks(nodes, edges)

	return stats
}
