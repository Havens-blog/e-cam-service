package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// K8sDeployment represents a simplified K8s Deployment with relevant metadata.
type K8sDeployment struct {
	Name        string
	Namespace   string
	Annotations map[string]string
}

// K8sClient defines the interface for querying K8s API.
type K8sClient interface {
	ListDeployments(ctx context.Context) ([]K8sDeployment, error)
}

// ServiceNameMapper builds a mapping from ARMS service names to K8s node IDs.
type ServiceNameMapper interface {
	BuildMapping(ctx context.Context, clusterName string) (map[string]string, error)
}

const armsAnnotationKey = "armsPilotCreateAppName"

// ---- 基于命名规则的映射器（默认，不需要连接 K8s 集群） ----

// NamingConventionMapper 基于 ARMS 服务名的命名规则推导 K8s 节点 ID。
// ARMS 的 armsPilotCreateAppName 格式通常为 "{env}_{deployment-name}"，
// 例如 "prod_sdp-model-analyzer" → namespace=prod, deployment=sdp-model-analyzer
// 如果没有下划线前缀，则使用 default namespace。
type NamingConventionMapper struct{}

// NewDefaultServiceNameMapper creates a mapper using naming convention (no K8s API needed).
func NewDefaultServiceNameMapper() *NamingConventionMapper {
	return &NamingConventionMapper{}
}

// BuildMapping parses ARMS service names from the trace data and converts them
// to K8s node IDs based on naming convention: "{env}_{name}" → k8s-{cluster}-{env}-{name}
// This is called with the service names already collected from ARMS traces,
// so we build the mapping lazily in Generate() instead.
// Here we return an identity mapping that will be populated by the caller.
func (m *NamingConventionMapper) BuildMapping(ctx context.Context, clusterName string) (map[string]string, error) {
	// 返回空映射，由 BuildMappingFromServices 填充
	return make(map[string]string), nil
}

// BuildMappingFromServices 从 ARMS 服务名列表构建映射。
// 命名规则：ARMS 服务名格式为 "{env}_{deployment-name}"
// 例如：prod_sdp-model-analyzer → 节点 ID: svc-sdp-model-analyzer
// 环境前缀被去掉，因为拓扑关心的是服务身份而非部署环境。
// 如果没有下划线，直接用原始名称。
func (m *NamingConventionMapper) BuildMappingFromServices(serviceNames []string, clusterName string) map[string]string {
	mapping := make(map[string]string, len(serviceNames))
	for _, svcName := range serviceNames {
		_, deployName := parseARMSServiceName(svcName)
		// 使用 svc- 前缀标识 APM 发现的服务节点
		nodeID := fmt.Sprintf("svc-%s", deployName)
		mapping[svcName] = nodeID
	}
	log.Printf("Built naming-convention mapping: %d ARMS services → node IDs", len(mapping))
	return mapping
}

// parseARMSServiceName 解析 ARMS 服务名，去掉环境前缀，提取 deployment name。
// 格式："{env}_{deployment-name}" → deployment-name
// 例如：prod_sdp-model-analyzer → sdp-model-analyzer
//
//	staging_user-service → user-service
//	gateway（无前缀）→ gateway
func parseARMSServiceName(armsName string) (env, deployName string) {
	idx := strings.Index(armsName, "_")
	if idx <= 0 {
		// 没有下划线或下划线在开头，整个名字就是 deployment name
		return "", armsName
	}
	return armsName[:idx], armsName[idx+1:]
}

// ---- 基于 K8s API 的映射器（K8s 集群内运行时使用） ----

// K8sAPIMapper implements ServiceNameMapper using K8s API to read Deployment annotations.
type K8sAPIMapper struct {
	k8sClient K8sClient
}

// NewServiceNameMapper creates a mapper with a custom K8s client (for testing or in-cluster use).
func NewServiceNameMapper(client K8sClient) *K8sAPIMapper {
	return &K8sAPIMapper{k8sClient: client}
}

// BuildMapping lists all Deployments in the cluster, reads the armsPilotCreateAppName
// annotation, and builds a mapping: ARMS service name → k8s-{cluster}-{namespace}-{name}.
func (m *K8sAPIMapper) BuildMapping(ctx context.Context, clusterName string) (map[string]string, error) {
	deployments, err := m.k8sClient.ListDeployments(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	mapping := make(map[string]string)
	for _, deploy := range deployments {
		armsName, ok := deploy.Annotations[armsAnnotationKey]
		if !ok || armsName == "" {
			continue
		}
		nodeID := FormatK8sNodeID(clusterName, deploy.Namespace, deploy.Name)
		mapping[armsName] = nodeID
	}

	log.Printf("Built K8s API mapping: %d ARMS services → K8s node IDs (from %d deployments)", len(mapping), len(deployments))
	return mapping, nil
}

// FormatK8sNodeID constructs a K8s node ID in the format k8s-{cluster}-{namespace}-{name}.
func FormatK8sNodeID(cluster, namespace, name string) string {
	return fmt.Sprintf("k8s-%s-%s-%s", cluster, namespace, name)
}
