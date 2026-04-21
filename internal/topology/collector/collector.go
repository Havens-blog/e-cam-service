package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository"
	"github.com/gotomicro/ego/core/elog"
)

// Collector 拓扑数据采集器接口
// 所有采集器实现此接口，输出统一的节点和边列表
type Collector interface {
	// Name 采集器名称
	Name() string
	// Collect 执行采集，返回节点和边列表
	Collect(ctx context.Context, tenantID string) ([]domain.TopoNode, []domain.TopoEdge, error)
}

// CollectorManager 采集器管理器，负责批量执行采集器并合并结果写入存储
type CollectorManager struct {
	collectors []Collector
	nodeRepo   repository.NodeRepository
	edgeRepo   repository.EdgeRepository
	logger     *elog.Component
}

// NewCollectorManager 创建采集器管理器
func NewCollectorManager(
	nodeRepo repository.NodeRepository,
	edgeRepo repository.EdgeRepository,
	collectors ...Collector,
) *CollectorManager {
	return &CollectorManager{
		collectors: collectors,
		nodeRepo:   nodeRepo,
		edgeRepo:   edgeRepo,
		logger:     elog.DefaultLogger,
	}
}

// RunAll 执行所有采集器，合并去重后写入 MongoDB
func (m *CollectorManager) RunAll(ctx context.Context, tenantID string) error {
	allNodes := make([]domain.TopoNode, 0)
	allEdges := make([]domain.TopoEdge, 0)

	for _, c := range m.collectors {
		m.logger.Info(fmt.Sprintf("running collector: %s", c.Name()))
		start := time.Now()

		nodes, edges, err := c.Collect(ctx, tenantID)
		if err != nil {
			m.logger.Error(fmt.Sprintf("collector %s failed: %v", c.Name(), err))
			continue // 单个采集器失败不影响其他
		}

		allNodes = append(allNodes, nodes...)
		allEdges = append(allEdges, edges...)
		m.logger.Info(fmt.Sprintf("collector %s done: %d nodes, %d edges, took %v",
			c.Name(), len(nodes), len(edges), time.Since(start)))
	}

	// 去重：按 ID 去重，后来的覆盖先来的
	nodeMap := make(map[string]domain.TopoNode, len(allNodes))
	for _, n := range allNodes {
		nodeMap[n.ID] = n
	}
	dedupNodes := make([]domain.TopoNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		dedupNodes = append(dedupNodes, n)
	}

	edgeMap := make(map[string]domain.TopoEdge, len(allEdges))
	for _, e := range allEdges {
		edgeMap[e.ID] = e
	}
	dedupEdges := make([]domain.TopoEdge, 0, len(edgeMap))
	for _, e := range edgeMap {
		dedupEdges = append(dedupEdges, e)
	}

	// 批量写入
	if err := m.nodeRepo.UpsertMany(ctx, dedupNodes); err != nil {
		return fmt.Errorf("failed to upsert nodes: %w", err)
	}
	if err := m.edgeRepo.UpsertMany(ctx, dedupEdges); err != nil {
		return fmt.Errorf("failed to upsert edges: %w", err)
	}

	m.logger.Info(fmt.Sprintf("collector run complete: %d nodes, %d edges written", len(dedupNodes), len(dedupEdges)))
	return nil
}
