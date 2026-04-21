package domain

import (
	"fmt"
	"time"
)

// 资源类型常量
const (
	NodeTypeDNSRecord      = "dns_record"
	NodeTypeCDN            = "cdn"
	NodeTypeWAF            = "waf"
	NodeTypeSLB            = "slb"
	NodeTypeALB            = "alb"
	NodeTypeELB            = "elb"
	NodeTypeGateway        = "gateway"
	NodeTypeK8sIngress     = "k8s_ingress"
	NodeTypeK8sService     = "k8s_service"
	NodeTypeK8sDeployment  = "k8s_deployment"
	NodeTypeK8sStatefulSet = "k8s_statefulset"
	NodeTypeECS            = "ecs"
	NodeTypeRDS            = "rds"
	NodeTypeRedis          = "redis"
	NodeTypeOSS            = "oss"
	NodeTypeS3             = "s3"
	NodeTypeExternal       = "external"
	NodeTypeUnknown        = "unknown"
)

// 资源分类常量
const (
	CategoryCompute    = "compute"
	CategoryNetwork    = "network"
	CategoryDatabase   = "database"
	CategoryStorage    = "storage"
	CategoryMiddleware = "middleware"
	CategorySecurity   = "security"
	CategoryContainer  = "container"
	CategoryGateway    = "gateway"
	CategoryDNS        = "dns"
)

// 云厂商常量
const (
	ProviderAliyun     = "aliyun"
	ProviderAWS        = "aws"
	ProviderTencent    = "tencent"
	ProviderHuawei     = "huawei"
	ProviderVolcano    = "volcano"
	ProviderSelfHosted = "self-hosted"
)

// 数据来源常量
const (
	SourceCloudAPI    = "cloud_api"
	SourceK8sAPI      = "k8s_api"
	SourceDeclaration = "declaration"
	SourceLog         = "log"
	SourceManual      = "manual"
	SourceDNSAPI      = "dns_api"
)

// 节点状态常量
const (
	StatusActive  = "active"
	StatusStopped = "stopped"
	StatusError   = "error"
	StatusUnknown = "unknown"
)

// ValidNodeTypes 所有合法的节点类型
var ValidNodeTypes = map[string]bool{
	NodeTypeDNSRecord: true, NodeTypeCDN: true, NodeTypeWAF: true,
	NodeTypeSLB: true, NodeTypeALB: true, NodeTypeELB: true,
	NodeTypeGateway: true, NodeTypeK8sIngress: true, NodeTypeK8sService: true,
	NodeTypeK8sDeployment: true, NodeTypeK8sStatefulSet: true,
	NodeTypeECS: true, NodeTypeRDS: true, NodeTypeRedis: true,
	NodeTypeOSS: true, NodeTypeS3: true, NodeTypeExternal: true, NodeTypeUnknown: true,
}

// ValidCategories 所有合法的资源分类
var ValidCategories = map[string]bool{
	CategoryCompute: true, CategoryNetwork: true, CategoryDatabase: true,
	CategoryStorage: true, CategoryMiddleware: true, CategorySecurity: true,
	CategoryContainer: true, CategoryGateway: true, CategoryDNS: true,
}

// ValidProviders 所有合法的云厂商
var ValidProviders = map[string]bool{
	ProviderAliyun: true, ProviderAWS: true, ProviderTencent: true,
	ProviderHuawei: true, ProviderVolcano: true, ProviderSelfHosted: true,
}

// ValidSourceCollectors 所有合法的数据来源
var ValidSourceCollectors = map[string]bool{
	SourceCloudAPI: true, SourceK8sAPI: true, SourceDeclaration: true,
	SourceLog: true, SourceManual: true, SourceDNSAPI: true,
}

// BidirectionalNodeTypes 通常应具有双向连接的节点类型（用于断链检测）
var BidirectionalNodeTypes = map[string]bool{
	NodeTypeSLB: true, NodeTypeALB: true, NodeTypeELB: true,
	NodeTypeGateway: true, NodeTypeK8sIngress: true, NodeTypeWAF: true,
	NodeTypeCDN: true,
}

// TopoNode 拓扑节点领域模型
type TopoNode struct {
	ID              string                 `bson:"_id" json:"id"`
	Name            string                 `bson:"name" json:"name"`
	Type            string                 `bson:"type" json:"type"`
	Category        string                 `bson:"category" json:"category"`
	Provider        string                 `bson:"provider" json:"provider"`
	Region          string                 `bson:"region" json:"region"`
	Status          string                 `bson:"status" json:"status"`
	SourceCollector string                 `bson:"source_collector" json:"source_collector"`
	Attributes      map[string]interface{} `bson:"attributes,omitempty" json:"attributes,omitempty"`
	TenantID        string                 `bson:"tenant_id" json:"tenant_id"`
	DagDepth        int                    `bson:"-" json:"dag_depth,omitempty"`
	UpdatedAt       time.Time              `bson:"updated_at" json:"updated_at"`
}

// Validate 校验节点字段合法性
func (n *TopoNode) Validate() error {
	if n.ID == "" {
		return fmt.Errorf("node id is required")
	}
	if n.Name == "" {
		return fmt.Errorf("node name is required")
	}
	if !ValidNodeTypes[n.Type] {
		return fmt.Errorf("invalid node type: %s", n.Type)
	}
	if !ValidCategories[n.Category] {
		return fmt.Errorf("invalid category: %s", n.Category)
	}
	if n.Provider != "" && !ValidProviders[n.Provider] {
		return fmt.Errorf("invalid provider: %s", n.Provider)
	}
	if n.SourceCollector != "" && !ValidSourceCollectors[n.SourceCollector] {
		return fmt.Errorf("invalid source_collector: %s", n.SourceCollector)
	}
	if n.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	return nil
}

// IsBidirectional 判断该节点类型是否通常应具有双向连接
func (n *TopoNode) IsBidirectional() bool {
	return BidirectionalNodeTypes[n.Type]
}

// IsDNSEntry 判断是否为 DNS 入口节点
func (n *TopoNode) IsDNSEntry() bool {
	return n.Type == NodeTypeDNSRecord
}
