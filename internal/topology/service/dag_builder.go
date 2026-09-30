package service

import (
	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
)

// DagBuilder DAG 构建器，负责计算节点深度和断链检测
type DagBuilder struct{}

// NewDagBuilder 创建 DAG 构建器
func NewDagBuilder() *DagBuilder {
	return &DagBuilder{}
}

// ComputeDepths 计算每个节点的 dag_depth（从 DNS 入口到该节点的最长路径长度）
// 算法：
// 1. 找出所有 type=dns_record 的节点作为入口（depth=0）
// 2. 如果没有 dns_record 节点，则找入度为 0 的节点作为入口
// 3. BFS 遍历，每个节点的 depth = max(所有入边来源节点的 depth) + 1
// 4. 环检测：如果 BFS 访问次数超过节点数 * 2，跳过（防止无限循环）
func (b *DagBuilder) ComputeDepths(nodes []domain.TopoNode, edges []domain.TopoEdge) {
	if len(nodes) == 0 {
		return
	}

	// 构建索引
	nodeIndex := make(map[string]int, len(nodes)) // node ID → nodes slice index
	for i := range nodes {
		nodeIndex[nodes[i].ID] = i
	}

	// 构建邻接表和入度表
	adj := make(map[string][]string)     // source → []target
	inDegree := make(map[string]int)     // node → 入度
	inEdges := make(map[string][]string) // target → []source

	for _, n := range nodes {
		inDegree[n.ID] = 0
	}
	for _, e := range edges {
		if e.Status == domain.EdgeStatusPending {
			continue // 跳过 pending 边
		}
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		inEdges[e.TargetID] = append(inEdges[e.TargetID], e.SourceID)
		if _, ok := inDegree[e.TargetID]; ok {
			inDegree[e.TargetID]++
		}
	}

	// 找入口节点：优先 dns_record 类型，否则入度为 0 的节点
	queue := make([]string, 0)
	depth := make(map[string]int)

	for _, n := range nodes {
		if n.Type == domain.NodeTypeDNSRecord {
			queue = append(queue, n.ID)
			depth[n.ID] = 0
		}
	}

	// 如果没有 DNS 入口，用入度为 0 的节点
	if len(queue) == 0 {
		for _, n := range nodes {
			if inDegree[n.ID] == 0 {
				queue = append(queue, n.ID)
				depth[n.ID] = 0
			}
		}
	}

	// 如果还是没有入口（全是环），取第一个节点
	if len(queue) == 0 && len(nodes) > 0 {
		queue = append(queue, nodes[0].ID)
		depth[nodes[0].ID] = 0
	}

	// BFS 计算最长路径深度
	maxIterations := len(nodes) * 3 // 环保护
	iterations := 0
	for len(queue) > 0 && iterations < maxIterations {
		curr := queue[0]
		queue = queue[1:]
		iterations++

		currDepth := depth[curr]
		for _, next := range adj[curr] {
			newDepth := currDepth + 1
			if existingDepth, visited := depth[next]; !visited || newDepth > existingDepth {
				depth[next] = newDepth
				queue = append(queue, next)
			}
		}
	}

	// 写回 depth 到节点
	for id, d := range depth {
		if idx, ok := nodeIndex[id]; ok {
			nodes[idx].DagDepth = d
		}
	}
}

// DetectBrokenLinks 统计断链边数：source 或 target 端点不在节点集内的边即断链。
// pending 边计入（其 target 尚未注册）。不再使用「双向类型只有单侧边」启发式。
func (b *DagBuilder) DetectBrokenLinks(nodes []domain.TopoNode, edges []domain.TopoEdge) int {
	return computeBrokenCount(nodes, edges)
}

// computeBrokenCount 悬空边计数：source_id 或 target_id 不在 nodes 内的边数（含 pending）。
func computeBrokenCount(nodes []domain.TopoNode, edges []domain.TopoEdge) int {
	nodeIDs := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		nodeIDs[n.ID] = struct{}{}
	}
	broken := 0
	for _, e := range edges {
		if _, ok := nodeIDs[e.SourceID]; !ok {
			broken++
			continue
		}
		if _, ok := nodeIDs[e.TargetID]; !ok {
			broken++
		}
	}
	return broken
}
