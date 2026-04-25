package service

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	// 0. 强制刷新：清除该域名的 LiveBuilder 缓存数据，强制重新从云 API 构建
	if params.Refresh && params.Domain != "" {
		s.clearLiveBuilderCache(ctx, params.TenantID, params.Domain)
	}

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

	// 未选域名时，默认只展示 APM 服务调用拓扑（避免全量数据不可读）
	if params.Domain == "" && params.SourceCollector == "" {
		nodeFilter.SourceCollectors = []string{domain.SourceAPM}
	}

	// 2. 查询 MongoDB 中的持久化节点（包括 APM 推送的数据）
	dbNodes, err := s.nodeRepo.Find(ctx, nodeFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	// 3. 检查当前查询的域名是否已有 DNS 入口节点。如果没有，用 LiveBuilder 构建并持久化
	hasDomainEntry := false
	if params.Domain != "" {
		for _, n := range dbNodes {
			if n.Type == domain.NodeTypeDNSRecord && n.Name == params.Domain {
				hasDomainEntry = true
				break
			}
		}
	} else {
		// 未指定域名时，只要有任何节点数据就不调 LiveBuilder
		hasDomainEntry = len(dbNodes) > 0
	}

	if !hasDomainEntry && s.liveBuilder != nil {
		liveGraph, liveErr := s.liveBuilder.BuildFromDNS(ctx, params.TenantID, params.Domain)
		if liveErr == nil && liveGraph != nil && len(liveGraph.Nodes) > 0 {
			// 异步持久化 LiveBuilder 结果到 MongoDB，下次请求直接走 DB
			go func() {
				bgCtx := context.Background()
				if err := s.nodeRepo.UpsertMany(bgCtx, liveGraph.Nodes); err != nil {
					// 持久化失败不影响本次请求
					_ = err
				}
				if err := s.edgeRepo.UpsertMany(bgCtx, liveGraph.Edges); err != nil {
					_ = err
				}
			}()

			// 将 LiveBuilder 结果合并到 dbNodes
			nodeMap := make(map[string]bool, len(dbNodes))
			for _, n := range dbNodes {
				nodeMap[n.ID] = true
			}
			for _, n := range liveGraph.Nodes {
				if !nodeMap[n.ID] {
					dbNodes = append(dbNodes, n)
				}
			}
			// 合并边到后续查询
			edgeFilter := domain.EdgeFilter{
				TenantID:   params.TenantID,
				HideSilent: params.HideSilent,
			}
			if params.SourceCollector != "" {
				edgeFilter.SourceCollectors = strings.Split(params.SourceCollector, ",")
			}
			dbEdges, _ := s.edgeRepo.Find(ctx, edgeFilter)

			// 合并 live 边和 db 边
			edgeMap := make(map[string]domain.TopoEdge)
			for _, e := range liveGraph.Edges {
				edgeMap[e.ID] = e
			}
			for _, e := range dbEdges {
				edgeMap[e.ID] = e
			}

			// 构建节点 ID 集合
			nodeIDSet := make(map[string]bool, len(dbNodes))
			for _, n := range dbNodes {
				nodeIDSet[n.ID] = true
			}

			edges := make([]domain.TopoEdge, 0, len(edgeMap))
			for _, e := range edgeMap {
				if nodeIDSet[e.SourceID] && (nodeIDSet[e.TargetID] || e.Status == domain.EdgeStatusPending) {
					edges = append(edges, e)
				}
			}

			s.builder.ComputeDepths(dbNodes, edges)
			// ELB→APM 服务桥接
			dbNodes, edges = s.bridgeELBToAPM(dbNodes, edges, params.TenantID)
			if params.Domain != "" {
				dbNodes, edges = s.filterByDomain(dbNodes, edges, params.Domain)
			}
			stats := s.computeStats(dbNodes, edges)
			return &domain.TopoGraph{Nodes: dbNodes, Edges: edges, Stats: stats}, nil
		}
	}

	// 4. 纯 MongoDB 路径（有基础设施节点，或 LiveBuilder 不可用）
	nodes := dbNodes

	if len(nodes) == 0 {
		return &domain.TopoGraph{
			Nodes: []domain.TopoNode{},
			Edges: []domain.TopoEdge{},
			Stats: domain.TopoStats{},
		}, nil
	}

	nodeIDSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeIDSet[n.ID] = true
	}

	edgeFilter := domain.EdgeFilter{
		TenantID:   params.TenantID,
		HideSilent: params.HideSilent,
	}
	if params.SourceCollector != "" {
		edgeFilter.SourceCollectors = strings.Split(params.SourceCollector, ",")
	} else if params.Domain == "" {
		// 未选域名时，边也只查 APM 来源
		edgeFilter.SourceCollectors = []string{domain.SourceAPM}
	}
	allEdges, err := s.edgeRepo.Find(ctx, edgeFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}
	edges := make([]domain.TopoEdge, 0, len(allEdges))
	for _, e := range allEdges {
		if nodeIDSet[e.SourceID] && (nodeIDSet[e.TargetID] || e.Status == domain.EdgeStatusPending) {
			edges = append(edges, e)
		}
	}

	// 6. 计算 DAG 深度
	s.builder.ComputeDepths(nodes, edges)

	// 6.5 ELB→APM 服务桥接：匹配 ELB 后端 ENI 的 IP 和 APM 服务的 service_ips
	nodes, edges = s.bridgeELBToAPM(nodes, edges, params.TenantID)

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
		if !visited[e.SourceID] || !visited[e.TargetID] {
			continue
		}
		// 非 APM 边：保留（基础设施配置边）
		if e.SourceCollector != domain.SourceAPM {
			filteredEdges = append(filteredEdges, e)
			continue
		}
		// APM 边：domains 为空（内部调用）保留
		if e.Attributes == nil || e.Attributes["domains"] == nil {
			filteredEdges = append(filteredEdges, e)
			continue
		}
		// APM 边：检查 domains 是否包含目标域名
		if domains, ok := e.Attributes["domains"].([]interface{}); ok {
			for _, d := range domains {
				if fmt.Sprint(d) == domainName {
					filteredEdges = append(filteredEdges, e)
					break
				}
			}
		} else if domains, ok := e.Attributes["domains"].([]string); ok {
			for _, d := range domains {
				if d == domainName {
					filteredEdges = append(filteredEdges, e)
					break
				}
			}
		}
	}

	return filteredNodes, filteredEdges
}

// bridgeELBToAPM 自动匹配 ELB 后端 ENI 的 IP 和 APM 服务的 service_ips，建立桥接边。
// 逻辑：遍历所有 ELB/SLB 类型的节点，找到其下游 ENI 节点的 private_ip，
// 然后匹配 APM 服务节点 attributes 中的 service_ips，匹配到就建立 ELB→APM 服务的边。
func (s *topologyService) bridgeELBToAPM(nodes []domain.TopoNode, edges []domain.TopoEdge, tenantID string) ([]domain.TopoNode, []domain.TopoEdge) {
	// 1. 收集所有 ENI 节点的 private_ip → 其父节点（ELB）的 ID
	eniParent := make(map[string]string) // ENI private_ip → ELB node ID
	nodeMap := make(map[string]*domain.TopoNode, len(nodes))
	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
	}

	// 找 ELB/SLB 类型节点的下游 ENI
	for _, e := range edges {
		srcNode := nodeMap[e.SourceID]
		tgtNode := nodeMap[e.TargetID]
		if srcNode == nil || tgtNode == nil {
			continue
		}
		// 源是 ELB/SLB/ALB 类型，目标是 ENI 类型
		isLB := srcNode.Type == domain.NodeTypeSLB || srcNode.Type == domain.NodeTypeALB ||
			srcNode.Type == domain.NodeTypeELB || strings.Contains(srcNode.Type, "lb")
		isENI := strings.Contains(tgtNode.Type, "eni")
		if !isLB || !isENI {
			continue
		}
		// 从 ENI 节点提取 private_ip
		if tgtNode.Attributes != nil {
			if ip, ok := tgtNode.Attributes["private_ip"].(string); ok && ip != "" {
				eniParent[ip] = srcNode.ID
			}
			// 也检查 private_ip_addresses 数组
			if ips, ok := tgtNode.Attributes["private_ip_addresses"]; ok {
				for _, addr := range extractIPsFromAttr(ips) {
					eniParent[addr] = srcNode.ID
				}
			}
		}
	}

	if len(eniParent) == 0 {
		return nodes, edges
	}

	// 2. 遍历 APM 服务节点，匹配 service_ips
	now := time.Now()
	seen := make(map[string]bool) // 防止重复边
	for i := range nodes {
		n := &nodes[i]
		if n.SourceCollector != domain.SourceAPM || n.Attributes == nil {
			continue
		}
		svcIPs, ok := n.Attributes["service_ips"]
		if !ok {
			continue
		}
		for _, ip := range extractIPsFromAttr(svcIPs) {
			if lbNodeID, matched := eniParent[ip]; matched {
				edgeID := fmt.Sprintf("bridge-%s-%s", lbNodeID, n.ID)
				if !seen[edgeID] {
					seen[edgeID] = true
					edges = append(edges, domain.TopoEdge{
						ID:              edgeID,
						SourceID:        lbNodeID,
						TargetID:        n.ID,
						Relation:        domain.RelationRoute,
						Direction:       domain.DirectionOutbound,
						SourceCollector: domain.SourceAPM,
						Status:          domain.EdgeStatusActive,
						TenantID:        tenantID,
						UpdatedAt:       now,
						Attributes: map[string]interface{}{
							"bridge_type": "elb_to_apm",
							"matched_ip":  ip,
						},
					})
				}
			}
		}
	}

	return nodes, edges
}

// extractIPsFromAttr 从属性值中提取 IP 列表（支持 string、[]string、[]interface{}）
func extractIPsFromAttr(val interface{}) []string {
	switch v := val.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []interface{}:
		ips := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				ips = append(ips, s)
			}
		}
		return ips
	}
	return nil
}

// clearLiveBuilderCache 清除指定域名的 LiveBuilder 缓存数据（dns_api 和 cloud_api 来源的节点和边）
func (s *topologyService) clearLiveBuilderCache(ctx context.Context, tenantID, domainName string) {
	// 找到该域名的 DNS 入口节点
	dnsNodeID := ""
	nodes, _ := s.nodeRepo.Find(ctx, domain.NodeFilter{TenantID: tenantID})
	for _, n := range nodes {
		if n.Type == domain.NodeTypeDNSRecord && n.Name == domainName {
			dnsNodeID = n.ID
			break
		}
	}
	if dnsNodeID == "" {
		return // 没有缓存数据，无需清理
	}

	// BFS 找到从该 DNS 节点可达的所有 LiveBuilder 产生的节点
	allEdges, _ := s.edgeRepo.Find(ctx, domain.EdgeFilter{TenantID: tenantID})
	adj := make(map[string][]string)
	for _, e := range allEdges {
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
	}

	visited := make(map[string]bool)
	queue := []string{dnsNodeID}
	visited[dnsNodeID] = true
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

	// 删除 LiveBuilder 产生的节点和边（dns_api / cloud_api 来源）
	for _, n := range nodes {
		if visited[n.ID] && (n.SourceCollector == domain.SourceDNSAPI || n.SourceCollector == domain.SourceCloudAPI) {
			_ = s.nodeRepo.Delete(ctx, n.ID)
		}
	}
	for _, e := range allEdges {
		if visited[e.SourceID] && (e.SourceCollector == domain.SourceDNSAPI || e.SourceCollector == domain.SourceCloudAPI) {
			_ = s.edgeRepo.Delete(ctx, e.ID)
		}
	}
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
