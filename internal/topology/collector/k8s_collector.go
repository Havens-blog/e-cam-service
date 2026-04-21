package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
)

// K8sResource K8s 资源（从 K8s API 采集的原始数据）
type K8sResource struct {
	Kind        string // Ingress / Service / Deployment / StatefulSet
	Name        string
	Namespace   string
	Cluster     string
	Labels      map[string]string
	Annotations map[string]string
	// Service 特有
	ServiceType string // ClusterIP / NodePort / LoadBalancer
	LBIngress   string // LoadBalancer 的外部地址（IP 或域名）
	ClusterIP   string
	Ports       []int32
	// Ingress 特有
	IngressRules []IngressRule
	// Deployment 特有
	Replicas      int32
	ReadyReplicas int32
	Selector      map[string]string
}

// IngressRule Ingress 路由规则
type IngressRule struct {
	Host        string
	Path        string
	ServiceName string
	ServicePort int32
}

// K8sProvider K8s 集群接口（由 client-go 实现）
type K8sProvider interface {
	// ListResources 获取指定集群的 K8s 资源
	ListResources(ctx context.Context, cluster string) ([]K8sResource, error)
	// ListClusters 获取所有集群
	ListClusters(ctx context.Context) ([]string, error)
}

// K8sCollector K8s 资源采集器
type K8sCollector struct {
	providers []K8sProvider
	provider  string // 云厂商标识
}

// NewK8sCollector 创建 K8s 采集器
func NewK8sCollector(cloudProvider string, providers ...K8sProvider) *K8sCollector {
	return &K8sCollector{providers: providers, provider: cloudProvider}
}

func (c *K8sCollector) Name() string { return "k8s_collector" }

// Collect 采集 K8s 资源并转换为拓扑节点和边
func (c *K8sCollector) Collect(ctx context.Context, tenantID string) ([]domain.TopoNode, []domain.TopoEdge, error) {
	nodes := make([]domain.TopoNode, 0)
	edges := make([]domain.TopoEdge, 0)

	for _, p := range c.providers {
		clusters, err := p.ListClusters(ctx)
		if err != nil {
			continue
		}

		for _, cluster := range clusters {
			resources, err := p.ListResources(ctx, cluster)
			if err != nil {
				continue
			}

			for _, res := range resources {
				n, e := c.convertResource(res, cluster, tenantID)
				nodes = append(nodes, n...)
				edges = append(edges, e...)
			}
		}
	}

	return nodes, edges, nil
}

func (c *K8sCollector) convertResource(res K8sResource, cluster, tenantID string) ([]domain.TopoNode, []domain.TopoEdge) {
	nodes := make([]domain.TopoNode, 0, 1)
	edges := make([]domain.TopoEdge, 0)
	now := time.Now()

	nodeID := fmt.Sprintf("k8s-%s-%s-%s", cluster, res.Namespace, res.Name)

	switch res.Kind {
	case "Service":
		node := domain.TopoNode{
			ID: nodeID, Name: res.Name,
			Type: domain.NodeTypeK8sService, Category: domain.CategoryContainer,
			Provider: c.provider, SourceCollector: domain.SourceK8sAPI,
			Status: domain.StatusActive, TenantID: tenantID, UpdatedAt: now,
			Attributes: map[string]interface{}{
				"namespace": res.Namespace, "cluster": cluster,
				"service_type": res.ServiceType, "cluster_ip": res.ClusterIP,
			},
		}
		nodes = append(nodes, node)

		// Service(LoadBalancer) → 关联到云 LB
		if res.ServiceType == "LoadBalancer" && res.LBIngress != "" {
			lbID := fmt.Sprintf("lb-%s", sanitizeID(res.LBIngress))
			edges = append(edges, domain.TopoEdge{
				ID:       fmt.Sprintf("e-%s-%s", lbID, nodeID),
				SourceID: lbID, TargetID: nodeID,
				Relation: domain.RelationRoute, Direction: domain.DirectionOutbound,
				SourceCollector: domain.SourceK8sAPI, Status: domain.EdgeStatusActive,
				TenantID: tenantID, UpdatedAt: now,
			})
		}

	case "Ingress":
		node := domain.TopoNode{
			ID: nodeID, Name: res.Name,
			Type: domain.NodeTypeK8sIngress, Category: domain.CategoryGateway,
			Provider: c.provider, SourceCollector: domain.SourceK8sAPI,
			Status: domain.StatusActive, TenantID: tenantID, UpdatedAt: now,
			Attributes: map[string]interface{}{
				"namespace": res.Namespace, "cluster": cluster,
			},
		}
		nodes = append(nodes, node)

		// Ingress rules → Service
		for _, rule := range res.IngressRules {
			svcID := fmt.Sprintf("k8s-%s-%s-%s", cluster, res.Namespace, rule.ServiceName)
			edges = append(edges, domain.TopoEdge{
				ID:       fmt.Sprintf("e-%s-%s", nodeID, svcID),
				SourceID: nodeID, TargetID: svcID,
				Relation: domain.RelationRoute, Direction: domain.DirectionOutbound,
				SourceCollector: domain.SourceK8sAPI, Status: domain.EdgeStatusActive,
				TenantID: tenantID, UpdatedAt: now,
			})
		}

	case "Deployment", "StatefulSet":
		nodeType := domain.NodeTypeK8sDeployment
		if res.Kind == "StatefulSet" {
			nodeType = domain.NodeTypeK8sStatefulSet
		}
		node := domain.TopoNode{
			ID: nodeID, Name: res.Name,
			Type: nodeType, Category: domain.CategoryContainer,
			Provider: c.provider, SourceCollector: domain.SourceK8sAPI,
			Status: domain.StatusActive, TenantID: tenantID, UpdatedAt: now,
			Attributes: map[string]interface{}{
				"namespace": res.Namespace, "cluster": cluster,
				"replicas": res.Replicas, "ready_replicas": res.ReadyReplicas,
			},
		}
		nodes = append(nodes, node)

		// Service → Deployment（通过 selector 匹配，这里简化为同名关联）
		svcID := fmt.Sprintf("k8s-%s-%s-%s", cluster, res.Namespace, res.Name)
		edges = append(edges, domain.TopoEdge{
			ID:       fmt.Sprintf("e-%s-%s", svcID, nodeID),
			SourceID: svcID, TargetID: nodeID,
			Relation: domain.RelationRoute, Direction: domain.DirectionOutbound,
			SourceCollector: domain.SourceK8sAPI, Status: domain.EdgeStatusPending, // pending 直到 Service 节点存在
			TenantID: tenantID, UpdatedAt: now,
		})
	}

	return nodes, edges
}
