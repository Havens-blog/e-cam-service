package main

import (
	"log"
	"strings"
	"time"
)

// LinkDeclaration mirrors the topology domain's LinkDeclaration for the push script.
// This is a local copy to avoid importing the full e-cam-service domain package.
type LinkDeclaration struct {
	Source    string            `json:"source"`
	Collector string            `json:"collector"`
	Node      DeclarationNode   `json:"node"`
	Links     []DeclarationLink `json:"links"`
	TenantID  string            `json:"tenant_id"`
}

// DeclarationNode represents a node in a link declaration.
type DeclarationNode struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Category   string                 `json:"category"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// DeclarationLink represents a link/edge in a link declaration.
type DeclarationLink struct {
	Target     string                 `json:"target"`
	TargetType string                 `json:"target_type"`
	Relation   string                 `json:"relation"`
	Direction  string                 `json:"direction"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// DeclarationGenerator converts ARMS data into LinkDeclaration list.
type DeclarationGenerator interface {
	Generate(
		deps []ServiceDependency,
		nameMapping map[string]string,
		edgeDomains map[string][]string,
		domainMetrics map[string]map[string]DomainMetric,
	) []LinkDeclaration
}

// DefaultDeclarationGenerator implements DeclarationGenerator.
type DefaultDeclarationGenerator struct {
	tenantID string
}

// NewDefaultDeclarationGenerator creates a new declaration generator.
func NewDefaultDeclarationGenerator(tenantID string) *DefaultDeclarationGenerator {
	return &DefaultDeclarationGenerator{tenantID: tenantID}
}

// Generate converts ARMS service dependencies into LinkDeclaration list.
// Groups by caller, maps service names to K8s node IDs, and attaches metrics + domains.
func (g *DefaultDeclarationGenerator) Generate(
	deps []ServiceDependency,
	nameMapping map[string]string,
	edgeDomains map[string][]string,
	domainMetrics map[string]map[string]DomainMetric,
) []LinkDeclaration {
	// Group dependencies by caller
	callerGroups := make(map[string][]ServiceDependency)
	for _, dep := range deps {
		callerGroups[dep.CallerServiceName] = append(callerGroups[dep.CallerServiceName], dep)
	}

	now := time.Now().Format(time.RFC3339)
	var declarations []LinkDeclaration

	for callerName, callees := range callerGroups {
		callerNodeID, ok := nameMapping[callerName]
		if !ok {
			log.Printf("WARN: unmapped ARMS service (caller), skipping: %s", callerName)
			continue
		}

		links := make([]DeclarationLink, 0, len(callees))
		for _, dep := range callees {
			calleeNodeID, ok := nameMapping[dep.CalleeServiceName]
			if !ok {
				log.Printf("WARN: unmapped ARMS service (callee), skipping: %s", dep.CalleeServiceName)
				continue
			}

			edgeKey := EdgeKey(dep.CallerServiceName, dep.CalleeServiceName)
			attrs := map[string]interface{}{
				"qps":          dep.QPS,
				"latency_p99":  dep.LatencyP99,
				"error_rate":   dep.ErrorRate,
				"last_seen_at": now,
			}

			// Attach domains if available
			if domains, ok := edgeDomains[edgeKey]; ok && len(domains) > 0 {
				attrs["domains"] = domains
			}

			// Attach domain metrics if available
			if dm, ok := domainMetrics[edgeKey]; ok && len(dm) > 0 {
				// Convert to map[string]interface{} for JSON serialization
				dmMap := make(map[string]interface{})
				for domain, metric := range dm {
					dmMap[domain] = map[string]interface{}{
						"qps":         metric.QPS,
						"latency_p99": metric.LatencyP99,
						"error_rate":  metric.ErrorRate,
					}
				}
				attrs["domain_metrics"] = dmMap
			}

			links = append(links, DeclarationLink{
				Target:     calleeNodeID,
				TargetType: "k8s_deployment",
				Relation:   "calls",
				Direction:  "outbound",
				Attributes: attrs,
			})
		}

		if len(links) == 0 {
			continue
		}

		declarations = append(declarations, LinkDeclaration{
			Source:    "arms-apm",
			Collector: "api",
			Node: DeclarationNode{
				ID:       callerNodeID,
				Name:     extractDeploymentName(callerNodeID),
				Type:     "k8s_deployment",
				Category: "container",
			},
			Links:    links,
			TenantID: g.tenantID,
		})
	}

	return declarations
}

// GenerateWithIPs 和 Generate 相同，但额外把每个服务的 ServiceIp 列表写到节点 attributes 的 service_ips 字段。
// 这些 IP 用于后端做 ELB→Gateway 的自动桥接匹配。
func (g *DefaultDeclarationGenerator) GenerateWithIPs(
	deps []ServiceDependency,
	nameMapping map[string]string,
	edgeDomains map[string][]string,
	domainMetrics map[string]map[string]DomainMetric,
	serviceIPs map[string][]string,
) []LinkDeclaration {
	declarations := g.Generate(deps, nameMapping, edgeDomains, domainMetrics)

	// 把 serviceIPs 写到对应节点的 attributes 里
	for i := range declarations {
		decl := &declarations[i]
		// 找到该节点对应的 ARMS 服务名
		for armsName, nodeID := range nameMapping {
			if nodeID == decl.Node.ID {
				if ips, ok := serviceIPs[armsName]; ok && len(ips) > 0 {
					// 节点 attributes 目前没有，需要用 map
					decl.Node.Attributes = map[string]interface{}{
						"service_ips": ips,
					}
				}
				break
			}
		}
	}

	return declarations
}

// extractDeploymentName extracts the deployment name from a K8s node ID.
// Format: k8s-{cluster}-{namespace}-{name} → returns {name}
func extractDeploymentName(nodeID string) string {
	parts := strings.Split(nodeID, "-")
	if len(parts) < 4 {
		return nodeID
	}
	// k8s-cluster-namespace-name (name may contain hyphens)
	// Skip "k8s", cluster, namespace → join the rest
	return strings.Join(parts[3:], "-")
}
