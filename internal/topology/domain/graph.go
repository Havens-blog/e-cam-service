package domain

// TopoGraph 拓扑图数据结构，包含节点、边和统计信息
type TopoGraph struct {
	Nodes []TopoNode `json:"nodes"`
	Edges []TopoEdge `json:"edges"`
	Stats TopoStats  `json:"stats"`
}

// TopoStats 拓扑统计信息
type TopoStats struct {
	NodeCount   int `json:"node_count"`
	EdgeCount   int `json:"edge_count"`
	DomainCount int `json:"domain_count"`
	BrokenCount int `json:"broken_count"`
	MaxDepth    int `json:"max_depth"`
}

// TopologyQueryParams 拓扑查询参数
type TopologyQueryParams struct {
	Mode            string `json:"mode"`             // business / instance
	Domain          string `json:"domain"`           // 按域名筛选（仅 business）
	ResourceID      string `json:"resource_id"`      // 资源 ID（仅 instance）
	Provider        string `json:"provider"`         // 云厂商过滤，逗号分隔
	Region          string `json:"region"`           // 地域过滤
	Type            string `json:"type"`             // 资源类型过滤
	SourceCollector string `json:"source_collector"` // 数据来源过滤
	HideSilent      bool   `json:"hide_silent"`      // 隐藏沉默链路
	TenantID        string `json:"tenant_id"`        // 租户 ID
}

// NodeFilter 节点查询过滤条件
type NodeFilter struct {
	TenantID         string
	Types            []string
	Categories       []string
	Providers        []string
	Regions          []string
	SourceCollectors []string
	IDs              []string
}

// EdgeFilter 边查询过滤条件
type EdgeFilter struct {
	TenantID         string
	SourceIDs        []string
	TargetIDs        []string
	Relations        []string
	SourceCollectors []string
	Statuses         []string
	HideSilent       bool
}

// DomainItem DNS 入口域名列表项
type DomainItem struct {
	Domain   string `json:"domain"`
	Provider string `json:"provider"`
	NodeID   string `json:"node_id"`
}

// NodeDetail 节点详情（含关联关系）
type NodeDetail struct {
	TopoNode
	UpstreamNodes   []TopoNode `json:"upstream_nodes"`
	DownstreamNodes []TopoNode `json:"downstream_nodes"`
	UpstreamEdges   []TopoEdge `json:"upstream_edges"`
	DownstreamEdges []TopoEdge `json:"downstream_edges"`
}
