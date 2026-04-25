package domain

import (
	"fmt"
	"time"
)

// LinkDeclaration 声明式拓扑注册数据
type LinkDeclaration struct {
	ID        string            `bson:"_id" json:"id"`
	Source    string            `bson:"source" json:"source"`       // 上报方标识
	Collector string            `bson:"collector" json:"collector"` // 采集方式: manual/api/agent/log
	Node      DeclarationNode   `bson:"node" json:"node"`
	Links     []DeclarationLink `bson:"links" json:"links"`
	TenantID  string            `bson:"tenant_id" json:"tenant_id"`
	CreatedAt time.Time         `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time         `bson:"updated_at" json:"updated_at"`
}

// DeclarationNode 声明中的节点定义
type DeclarationNode struct {
	ID         string                 `bson:"id" json:"id"`
	Name       string                 `bson:"name" json:"name"`
	Type       string                 `bson:"type" json:"type"`
	Category   string                 `bson:"category" json:"category"`
	Provider   string                 `bson:"provider" json:"provider"`
	Region     string                 `bson:"region" json:"region"`
	Attributes map[string]interface{} `bson:"attributes,omitempty" json:"attributes,omitempty"`
}

// DeclarationLink 声明中的连线定义
type DeclarationLink struct {
	Target     string                 `bson:"target" json:"target"`
	TargetType string                 `bson:"target_type" json:"target_type"`
	Relation   string                 `bson:"relation" json:"relation"`
	Direction  string                 `bson:"direction" json:"direction"`
	Attributes map[string]interface{} `bson:"attributes,omitempty" json:"attributes,omitempty"`
}

// ValidCollectorTypes 合法的采集方式
var ValidCollectorTypes = map[string]bool{
	"manual": true, "api": true, "agent": true, "log": true,
}

// Validate 校验声明数据合法性
func (d *LinkDeclaration) Validate() error {
	if d.Source == "" {
		return fmt.Errorf("source is required")
	}
	if d.Collector != "" && !ValidCollectorTypes[d.Collector] {
		return fmt.Errorf("invalid collector type: %s", d.Collector)
	}
	if d.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}

	// 校验节点
	if d.Node.ID == "" {
		return fmt.Errorf("node.id is required")
	}
	if d.Node.Name == "" {
		return fmt.Errorf("node.name is required")
	}
	if !ValidNodeTypes[d.Node.Type] {
		return fmt.Errorf("invalid node.type: %s", d.Node.Type)
	}
	if !ValidCategories[d.Node.Category] {
		return fmt.Errorf("invalid node.category: %s", d.Node.Category)
	}

	// 校验连线
	for i, link := range d.Links {
		if link.Target == "" {
			return fmt.Errorf("links[%d].target is required", i)
		}
		if !ValidRelations[link.Relation] {
			return fmt.Errorf("links[%d].relation is invalid: %s", i, link.Relation)
		}
		if link.Direction != "" && !ValidDirections[link.Direction] {
			return fmt.Errorf("links[%d].direction is invalid: %s", i, link.Direction)
		}
	}

	return nil
}

// ToTopoNode 将声明节点转换为拓扑节点
func (d *LinkDeclaration) ToTopoNode() TopoNode {
	collector := SourceDeclaration
	switch {
	case d.Collector == "log":
		collector = SourceLog
	case d.Source == "arms-apm":
		collector = SourceAPM
	}
	return TopoNode{
		ID:              d.Node.ID,
		Name:            d.Node.Name,
		Type:            d.Node.Type,
		Category:        d.Node.Category,
		Provider:        d.Node.Provider,
		Region:          d.Node.Region,
		Status:          StatusActive,
		SourceCollector: collector,
		Attributes:      d.Node.Attributes,
		TenantID:        d.TenantID,
		UpdatedAt:       time.Now(),
	}
}

// ToTopoEdges 将声明连线转换为拓扑边列表
func (d *LinkDeclaration) ToTopoEdges() []TopoEdge {
	edges := make([]TopoEdge, 0, len(d.Links))
	collector := SourceDeclaration
	switch {
	case d.Collector == "log":
		collector = SourceLog
	case d.Source == "arms-apm":
		collector = SourceAPM
	}
	for _, link := range d.Links {
		edge := TopoEdge{
			ID:              fmt.Sprintf("e-%s-%s", d.Node.ID, link.Target),
			SourceID:        d.Node.ID,
			TargetID:        link.Target,
			Relation:        link.Relation,
			Direction:       link.Direction,
			SourceCollector: collector,
			Attributes:      link.Attributes,
			Status:          EdgeStatusActive, // 默认 active，后续检查目标节点是否存在
			TenantID:        d.TenantID,
			UpdatedAt:       time.Now(),
		}
		edges = append(edges, edge)
	}
	return edges
}
