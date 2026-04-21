package web

import (
	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
)

// TopologyQueryVO 拓扑查询请求参数
type TopologyQueryVO struct {
	Mode            string `form:"mode" json:"mode"`                         // business / instance
	Domain          string `form:"domain" json:"domain"`                     // 按域名筛选
	ResourceID      string `form:"resource_id" json:"resource_id"`           // 资源 ID（instance 模式）
	Provider        string `form:"provider" json:"provider"`                 // 云厂商过滤
	Region          string `form:"region" json:"region"`                     // 地域过滤
	Type            string `form:"type" json:"type"`                         // 资源类型过滤
	SourceCollector string `form:"source_collector" json:"source_collector"` // 数据来源过滤
	HideSilent      bool   `form:"hide_silent" json:"hide_silent"`           // 隐藏沉默链路
}

// ToParams 转换为领域层查询参数
func (v *TopologyQueryVO) ToParams(tenantID string) domain.TopologyQueryParams {
	mode := v.Mode
	if mode == "" {
		mode = "business"
	}
	return domain.TopologyQueryParams{
		Mode:            mode,
		Domain:          v.Domain,
		ResourceID:      v.ResourceID,
		Provider:        v.Provider,
		Region:          v.Region,
		Type:            v.Type,
		SourceCollector: v.SourceCollector,
		HideSilent:      v.HideSilent,
		TenantID:        tenantID,
	}
}

// DeclarationRequestVO 声明式注册请求体
type DeclarationRequestVO struct {
	Source    string              `json:"source" binding:"required"`
	Collector string              `json:"collector"`
	Node      DeclarationNodeVO   `json:"node" binding:"required"`
	Links     []DeclarationLinkVO `json:"links"`
}

// DeclarationNodeVO 声明节点 VO
type DeclarationNodeVO struct {
	ID         string                 `json:"id" binding:"required"`
	Name       string                 `json:"name" binding:"required"`
	Type       string                 `json:"type" binding:"required"`
	Category   string                 `json:"category" binding:"required"`
	Provider   string                 `json:"provider"`
	Region     string                 `json:"region"`
	Attributes map[string]interface{} `json:"attributes"`
}

// DeclarationLinkVO 声明连线 VO
type DeclarationLinkVO struct {
	Target     string                 `json:"target" binding:"required"`
	TargetType string                 `json:"target_type"`
	Relation   string                 `json:"relation" binding:"required"`
	Direction  string                 `json:"direction"`
	Attributes map[string]interface{} `json:"attributes"`
}

// ToDeclaration 转换为领域模型
func (v *DeclarationRequestVO) ToDeclaration(tenantID string) domain.LinkDeclaration {
	links := make([]domain.DeclarationLink, 0, len(v.Links))
	for _, l := range v.Links {
		links = append(links, domain.DeclarationLink{
			Target:     l.Target,
			TargetType: l.TargetType,
			Relation:   l.Relation,
			Direction:  l.Direction,
			Attributes: l.Attributes,
		})
	}
	return domain.LinkDeclaration{
		Source:    v.Source,
		Collector: v.Collector,
		Node: domain.DeclarationNode{
			ID:         v.Node.ID,
			Name:       v.Node.Name,
			Type:       v.Node.Type,
			Category:   v.Node.Category,
			Provider:   v.Node.Provider,
			Region:     v.Node.Region,
			Attributes: v.Node.Attributes,
		},
		Links:    links,
		TenantID: tenantID,
	}
}

// --- Response VOs ---

// TopologyResponseVO 拓扑图响应
type TopologyResponseVO struct {
	Nodes []domain.TopoNode `json:"nodes"`
	Edges []domain.TopoEdge `json:"edges"`
	Stats domain.TopoStats  `json:"stats"`
}

// DomainListResponseVO 域名列表响应
type DomainListResponseVO struct {
	Domains []domain.DomainItem `json:"domains"`
}

// NodeDetailResponseVO 节点详情响应
type NodeDetailResponseVO struct {
	domain.NodeDetail
}

// StatsResponseVO 统计信息响应
type StatsResponseVO struct {
	domain.TopoStats
}
