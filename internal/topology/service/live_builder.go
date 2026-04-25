package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// dnsRecordDoc c_dns_record 文档
type dnsRecordDoc struct {
	RecordID string `bson:"record_id"`
	Domain   string `bson:"domain"`
	RR       string `bson:"rr"`
	Type     string `bson:"type"`
	Value    string `bson:"value"`
	TTL      int    `bson:"ttl"`
	Provider string `bson:"provider"`
	TenantID string `bson:"tenant_id"`
}

// cmdbInstanceDoc c_instance 文档（简化版，只取拓扑需要的字段）
type cmdbInstanceDoc struct {
	ID         int64                  `bson:"id"`
	ModelUID   string                 `bson:"model_uid"`
	AssetID    string                 `bson:"asset_id"`
	AssetName  string                 `bson:"asset_name"`
	TenantID   string                 `bson:"tenant_id"`
	Attributes map[string]interface{} `bson:"attributes"`
}

// cmdbRelationDoc c_instance_relation 文档
type cmdbRelationDoc struct {
	SourceInstanceID int64  `bson:"source_instance_id"`
	TargetInstanceID int64  `bson:"target_instance_id"`
	RelationTypeUID  string `bson:"relation_type_uid"`
}

// LiveTopologyBuilder 实时拓扑构建器
type LiveTopologyBuilder struct {
	db      *mongox.Mongo
	builder *DagBuilder
	logger  func(msg string, args ...interface{})
}

func NewLiveTopologyBuilder(db *mongox.Mongo) *LiveTopologyBuilder {
	return &LiveTopologyBuilder{db: db, builder: NewDagBuilder()}
}

// BuildFromDNS 从 DNS 记录 + CMDB 实例实时构建全链路拓扑
func (b *LiveTopologyBuilder) BuildFromDNS(ctx context.Context, tenantID string, domainFilter string) (*domain.TopoGraph, error) {
	now := time.Now()
	nodes := make([]domain.TopoNode, 0)
	edges := make([]domain.TopoEdge, 0)
	seen := make(map[string]bool)

	// 1. 查询匹配的 DNS 记录
	records, err := b.queryDNSRecords(ctx, tenantID, domainFilter)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return &domain.TopoGraph{Nodes: []domain.TopoNode{}, Edges: []domain.TopoEdge{}, Stats: domain.TopoStats{}}, nil
	}

	// 2. 按需加载 CMDB 实例（只查询当前域名相关的实例）
	domainNames := make([]string, 0)
	cnameValues := make([]string, 0)
	aRecordIPs := make([]string, 0)
	for _, rec := range records {
		fullDomain := rec.Domain
		if rec.RR != "" && rec.RR != "@" {
			fullDomain = rec.RR + "." + rec.Domain
		}
		domainNames = append(domainNames, fullDomain)
		if rec.Type == "CNAME" {
			cnameValues = append(cnameValues, strings.TrimRight(rec.Value, "."))
		} else if rec.Type == "A" || rec.Type == "AAAA" {
			aRecordIPs = append(aRecordIPs, rec.Value)
		}
	}
	// 提取 CNAME 前缀（如 ALB/ELB ID）
	for _, cv := range cnameValues {
		prefix := extractCDNDomainPrefix(strings.ToLower(cv))
		if prefix != "" && prefix != cv {
			cnameValues = append(cnameValues, prefix)
		}
	}
	instances, err := b.queryInstancesByDomain(ctx, tenantID, domainNames, cnameValues, aRecordIPs)
	if err != nil {
		return nil, err
	}
	// 从已加载的实例中提取下游地址，继续查询关联实例
	instances, err = b.expandDownstream(ctx, tenantID, instances)
	if err != nil {
		// 扩展失败不阻塞
	}
	// 构建匹配索引
	ipIndex := make(map[string]*cmdbInstanceDoc)    // IP → instance
	cnameIndex := make(map[string]*cmdbInstanceDoc) // CNAME 地址 → instance（精确匹配用）
	idIndex := make(map[int64]*cmdbInstanceDoc)     // ID → instance
	for i := range instances {
		inst := &instances[i]
		idIndex[inst.ID] = inst
		// 按 asset_id 也建索引（后端服务器列表可能用 instance_id 引用）
		if inst.AssetID != "" {
			ipIndex[inst.AssetID] = inst
		}
		// 提取 IP 地址
		for _, key := range []string{"ip_address", "public_ip", "private_ip", "primary_private_ip", "vip", "address", "ip"} {
			if ip := getStr(inst.Attributes, key); ip != "" {
				ipIndex[ip] = inst
			}
		}
		// 提取 CNAME 地址（仅用于精确匹配，不包含 domain_name）
		for _, key := range []string{"cname", "dns_name", "endpoint", "public_dns"} {
			if cn := getStr(inst.Attributes, key); cn != "" {
				// 支持分号分隔的多个 CNAME（如华为云 WAF 有 old/new 两个 CNAME）
				for _, c := range strings.Split(cn, ";") {
					c = strings.TrimSpace(c)
					if c != "" {
						cnameIndex[strings.ToLower(c)] = inst
					}
				}
			}
		}
		// CDN/WAF 的 domain_name 是加速域名/防护域名，不是 CNAME 地址
		// 但 CDN 的 CNAME 通常以 domain_name 为前缀（如 www.example.com.c.vedcdnlb.com）
		// 这种匹配在 matchInstance 的 CDN 前缀匹配中处理
	}

	// 构建 CDN 域名索引：domain_name → []*cmdbInstanceDoc（同一域名可能有多个云厂商的 CDN）
	cdnDomainIndex := make(map[string][]*cmdbInstanceDoc)
	for i := range instances {
		inst := &instances[i]
		if strings.Contains(inst.ModelUID, "cdn") {
			if dn := getStr(inst.Attributes, "domain_name"); dn != "" {
				key := strings.ToLower(dn)
				cdnDomainIndex[key] = append(cdnDomainIndex[key], inst)
			}
		}
	}

	// 构建 WAF 域名索引：domain_name → *cmdbInstanceDoc
	// WAF 实例可能通过 protected_hosts 或 domain_name 关联到域名
	wafDomainIndex := make(map[string]*cmdbInstanceDoc)
	for i := range instances {
		inst := &instances[i]
		if !strings.Contains(inst.ModelUID, "waf") {
			continue
		}
		// protected_hosts 是域名列表
		if hosts, ok := inst.Attributes["protected_hosts"]; ok {
			if hostList, ok := hosts.([]interface{}); ok {
				for _, h := range hostList {
					if hs, ok := h.(string); ok && hs != "" {
						wafDomainIndex[strings.ToLower(hs)] = inst
					}
				}
			}
		}
	}

	// 3. 预加载 CMDB 关系
	relations, err := b.queryRelations(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// 构建下游索引：source_id → []target_id
	downstreamMap := make(map[int64][]int64)
	for _, rel := range relations {
		downstreamMap[rel.SourceInstanceID] = append(downstreamMap[rel.SourceInstanceID], rel.TargetInstanceID)
	}

	// 4. 构建拓扑：DNS → 匹配的云资源 → 递归追踪下游
	for _, rec := range records {
		fullDomain := rec.Domain
		if rec.RR != "" && rec.RR != "@" {
			fullDomain = rec.RR + "." + rec.Domain
		}

		// DNS 入口节点
		dnsNodeID := fmt.Sprintf("dns-%s", fullDomain)
		if !seen[dnsNodeID] {
			seen[dnsNodeID] = true
			nodes = append(nodes, domain.TopoNode{
				ID: dnsNodeID, Name: fullDomain, Type: domain.NodeTypeDNSRecord,
				Category: domain.CategoryDNS, Provider: rec.Provider,
				Status: domain.StatusActive, SourceCollector: domain.SourceDNSAPI,
				TenantID: tenantID, UpdatedAt: now,
				Attributes: map[string]interface{}{"record_type": rec.Type, "record_value": rec.Value, "rr": rec.RR},
			})
		}

		// 匹配 DNS value → CMDB 实例
		matched := b.matchInstance(rec, ipIndex, cnameIndex, cdnDomainIndex)
		if matched != nil {
			// 添加匹配到的云资源节点
			instNodeID := fmt.Sprintf("inst-%d", matched.ID)
			if !seen[instNodeID] {
				seen[instNodeID] = true
				nodes = append(nodes, instanceToTopoNode(matched, now))
			}
			edges = append(edges, domain.TopoEdge{
				ID: fmt.Sprintf("e-%s-%s", dnsNodeID, instNodeID), SourceID: dnsNodeID, TargetID: instNodeID,
				Relation: domain.RelationResolve, Direction: domain.DirectionOutbound,
				SourceCollector: domain.SourceDNSAPI, Status: domain.EdgeStatusActive,
				TenantID: tenantID, UpdatedAt: now,
			})
			// 递归追踪下游
			b.traceDownstream(matched.ID, instNodeID, idIndex, downstreamMap, ipIndex, cnameIndex, wafDomainIndex, &nodes, &edges, seen, tenantID, now, 1, 5)
		} else {
			// 未匹配到 CMDB 实例，创建外部/未知节点
			extID := fmt.Sprintf("ext-%s", sanitize(rec.Value))
			if !seen[extID] {
				seen[extID] = true
				nodes = append(nodes, domain.TopoNode{
					ID: extID, Name: rec.Value, Type: domain.NodeTypeExternal,
					Category: domain.CategoryNetwork, SourceCollector: domain.SourceDNSAPI,
					Status: domain.StatusActive, TenantID: tenantID, UpdatedAt: now,
				})
			}
			edges = append(edges, domain.TopoEdge{
				ID: fmt.Sprintf("e-%s-%s", dnsNodeID, extID), SourceID: dnsNodeID, TargetID: extID,
				Relation: domain.RelationResolve, Direction: domain.DirectionOutbound,
				SourceCollector: domain.SourceDNSAPI, Status: domain.EdgeStatusActive,
				TenantID: tenantID, UpdatedAt: now,
			})
		}
	}

	// 5. 计算 DAG 深度
	b.builder.ComputeDepths(nodes, edges)

	stats := domain.TopoStats{
		NodeCount: len(nodes), EdgeCount: len(edges),
		DomainCount: countType(nodes, domain.NodeTypeDNSRecord),
		BrokenCount: b.builder.DetectBrokenLinks(nodes, edges),
		MaxDepth:    maxDepthOf(nodes),
	}
	return &domain.TopoGraph{Nodes: nodes, Edges: edges, Stats: stats}, nil
}

// traceDownstream 递归追踪 CMDB 实例的下游关系
// 两条路径并行发现下游：
// 1. CMDB 关系（c_instance_relation）
// 2. 属性级联匹配（从实例属性中提取下游地址，再匹配 CMDB 实例）
func (b *LiveTopologyBuilder) traceDownstream(
	instanceID int64, parentNodeID string,
	idIndex map[int64]*cmdbInstanceDoc, downstreamMap map[int64][]int64,
	ipIndex map[string]*cmdbInstanceDoc, cnameIndex map[string]*cmdbInstanceDoc,
	wafDomainIndex map[string]*cmdbInstanceDoc,
	nodes *[]domain.TopoNode, edges *[]domain.TopoEdge,
	seen map[string]bool, tenantID string, now time.Time,
	depth, maxDepth int,
) {
	if depth > maxDepth {
		return
	}

	// 防止递归重入：用 "traced-{instanceID}" 标记已追踪的实例
	traceKey := fmt.Sprintf("traced-%d", instanceID)
	if seen[traceKey] {
		return
	}
	seen[traceKey] = true

	// 收集所有下游实例（去重）
	downstreamInstances := make(map[int64]bool)

	// 路径 1：CMDB 关系
	for _, targetID := range downstreamMap[instanceID] {
		downstreamInstances[targetID] = true
	}

	// 路径 2：属性级联匹配
	inst := idIndex[instanceID]
	if inst != nil {
		downstreamAddrs := extractDownstreamAddresses(inst)
		// 去重
		addrSeen := make(map[string]bool)
		uniqueAddrs := make([]string, 0, len(downstreamAddrs))
		for _, a := range downstreamAddrs {
			al := strings.ToLower(strings.TrimRight(a, "."))
			if !addrSeen[al] {
				addrSeen[al] = true
				uniqueAddrs = append(uniqueAddrs, a)
			}
		}

		for _, addr := range uniqueAddrs {
			matched := matchByAddress(addr, ipIndex, cnameIndex)
			if matched != nil && matched.ID != instanceID {
				// ENI 透传：如果匹配到 ENI，尝试找到绑定的 ECS
				if strings.Contains(matched.ModelUID, "eni") {
					ecsID := getStr(matched.Attributes, "instance_id")
					if ecsID == "" {
						ecsID = getStr(matched.Attributes, "instanceid")
					}
					if ecsID != "" {
						if ecsInst := ipIndex[ecsID]; ecsInst != nil {
							downstreamInstances[ecsInst.ID] = true
							continue
						}
					}
					// ENI 没有绑定 ECS 信息，也尝试用 ENI 的 private_ip 匹配 ECS
					eniIP := getStr(matched.Attributes, "private_ip")
					if eniIP != "" {
						if ecsInst := ipIndex[eniIP]; ecsInst != nil && ecsInst.ID != matched.ID {
							downstreamInstances[ecsInst.ID] = true
							continue
						}
					}
				}
				downstreamInstances[matched.ID] = true
			} else if addr != "" && (matched == nil || matched.ID == instanceID) {
				// 未匹配到 CMDB 实例（或匹配到自身）
				// 先尝试通过 CDN/WAF CNAME 前缀匹配 WAF 实例
				addrLower := strings.ToLower(strings.TrimRight(addr, "."))
				prefix := extractCDNDomainPrefix(addrLower)
				if prefix != "" {
					if wafInst := wafDomainIndex[prefix]; wafInst != nil && wafInst.ID != instanceID {
						// 匹配到 WAF 实例
						downstreamInstances[wafInst.ID] = true
						continue
					}
				}
				// 创建外部节点（保持链路可见性）
				extID := fmt.Sprintf("ext-%s", sanitize(addr))
				if !seen[extID] {
					seen[extID] = true
					extType := domain.NodeTypeExternal
					extCategory := domain.CategoryNetwork
					extProvider := ""
					// 尝试从地址识别类型和云厂商
					addrLower := strings.ToLower(addr)
					if strings.Contains(addrLower, "waf") || strings.Contains(addrLower, "yundun") {
						extType = domain.NodeTypeWAF
						extCategory = domain.CategorySecurity
					} else if strings.Contains(addrLower, "cdn") || strings.Contains(addrLower, "dcdn") {
						extType = domain.NodeTypeCDN
					} else if strings.Contains(addrLower, "alb") {
						extType = "alb"
					} else if strings.Contains(addrLower, "nlb") {
						extType = "nlb"
					} else if strings.Contains(addrLower, "slb") || strings.Contains(addrLower, "clb") || strings.Contains(addrLower, "elb") {
						extType = domain.NodeTypeSLB
					}
					// 从 CNAME 后缀识别云厂商
					_, p := extractCDNDomainPrefixAndProvider(addrLower)
					if p != "" {
						extProvider = p
					}
					// 生成简短名称：从 CNAME 中提取实例标识
					extName := addr
					if dotIdx := strings.Index(addr, "."); dotIdx > 0 {
						extName = addr[:dotIdx] // 取第一段作为简短名称
					}
					*nodes = append(*nodes, domain.TopoNode{
						ID: extID, Name: extName, Type: extType,
						Category: extCategory, Provider: extProvider,
						SourceCollector: domain.SourceCloudAPI,
						Status:          domain.StatusActive, TenantID: tenantID, UpdatedAt: now,
						Attributes: map[string]interface{}{"full_address": addr},
					})
				}
				edgeID := fmt.Sprintf("e-%s-%s", parentNodeID, extID)
				if !seen[edgeID] {
					seen[edgeID] = true
					*edges = append(*edges, domain.TopoEdge{
						ID: edgeID, SourceID: parentNodeID, TargetID: extID,
						Relation: domain.RelationRoute, Direction: domain.DirectionOutbound,
						SourceCollector: domain.SourceCloudAPI, Status: domain.EdgeStatusActive,
						TenantID: tenantID, UpdatedAt: now,
					})
				}
				// 对外部 WAF/CDN 节点，尝试从其地址中提取下游 ALB/SLB 实例
				// 例如 WAF 的源站可能是 ALB CNAME: alb-xxx.cn-guangzhou.volcenginealb.com
				// 这里不做（外部节点没有 source_ips 属性），但如果 WAF 实例在 CMDB 中
				// 且有 source_ips，会在 wafDomainIndex 匹配时处理
			}
		}
	}

	// 遍历所有下游 — 同类型实例聚合显示（如 LB 的多个 ECS 后端聚合为一个节点）
	// 按类型分组
	typeGroups := make(map[string][]int64) // nodeType → []instanceID
	for targetID := range downstreamInstances {
		target := idIndex[targetID]
		if target == nil {
			continue
		}
		nodeType, _ := modelUIDToType(target.ModelUID)
		typeGroups[nodeType] = append(typeGroups[nodeType], targetID)
	}

	for nodeType, targetIDs := range typeGroups {
		// 同类型超过 10 个实例时聚合显示（与需求文档一致）
		if len(targetIDs) > 10 && (nodeType == domain.NodeTypeECS || nodeType == domain.NodeTypeRDS || nodeType == domain.NodeTypeRedis) {
			// 聚合节点
			aggID := fmt.Sprintf("agg-%s-%s", parentNodeID, nodeType)
			if !seen[aggID] {
				seen[aggID] = true
				provider := ""
				var sampleNames []string
				for _, tid := range targetIDs {
					t := idIndex[tid]
					if t != nil {
						if provider == "" {
							provider = extractProvider(t.ModelUID)
						}
						name := t.AssetName
						if name == "" {
							name = t.AssetID
						}
						if len(sampleNames) < 3 {
							sampleNames = append(sampleNames, name)
						}
					}
				}
				aggName := fmt.Sprintf("%s × %d", strings.ToUpper(nodeType), len(targetIDs))
				*nodes = append(*nodes, domain.TopoNode{
					ID: aggID, Name: aggName, Type: nodeType,
					Category: domain.CategoryCompute, Provider: provider,
					Status: domain.StatusActive, SourceCollector: domain.SourceCloudAPI,
					TenantID: tenantID, UpdatedAt: now,
					Attributes: map[string]interface{}{
						"is_aggregated": true,
						"count":         len(targetIDs),
						"sample_names":  sampleNames,
					},
				})
			}
			edgeID := fmt.Sprintf("e-%s-%s", parentNodeID, aggID)
			if !seen[edgeID] {
				seen[edgeID] = true
				*edges = append(*edges, domain.TopoEdge{
					ID: edgeID, SourceID: parentNodeID, TargetID: aggID,
					Relation: domain.RelationRoute, Direction: domain.DirectionOutbound,
					SourceCollector: domain.SourceCloudAPI, Status: domain.EdgeStatusActive,
					TenantID: tenantID, UpdatedAt: now,
				})
			}
		} else {
			// 少量实例逐个显示
			for _, targetID := range targetIDs {
				target := idIndex[targetID]
				if target == nil {
					continue
				}
				targetNodeID := fmt.Sprintf("inst-%d", target.ID)
				if !seen[targetNodeID] {
					seen[targetNodeID] = true
					*nodes = append(*nodes, instanceToTopoNode(target, now))
				}
				edgeID := fmt.Sprintf("e-%s-%s", parentNodeID, targetNodeID)
				if !seen[edgeID] {
					seen[edgeID] = true
					*edges = append(*edges, domain.TopoEdge{
						ID: edgeID, SourceID: parentNodeID, TargetID: targetNodeID,
						Relation: domain.RelationRoute, Direction: domain.DirectionOutbound,
						SourceCollector: domain.SourceCloudAPI, Status: domain.EdgeStatusActive,
						TenantID: tenantID, UpdatedAt: now,
					})
				}
				// 递归
				b.traceDownstream(target.ID, targetNodeID, idIndex, downstreamMap, ipIndex, cnameIndex, wafDomainIndex, nodes, edges, seen, tenantID, now, depth+1, maxDepth)
			}
		}
	}
}

// extractDownstreamAddresses 从实例属性中提取下游地址（IP 或域名）
// 不同资源类型的下游地址存在不同的属性字段中
func extractDownstreamAddresses(inst *cmdbInstanceDoc) []string {
	var addrs []string
	attrs := inst.Attributes
	if attrs == nil {
		return addrs
	}

	// CDN 回源地址
	for _, key := range []string{"origins", "origin_host", "origin_server", "origin_servers", "source_domain", "back_to_origin_host"} {
		addrs = append(addrs, extractAddrsFromAttr(attrs, key)...)
	}
	// WAF 回源 IP/域名
	for _, key := range []string{"source_ips", "source_ip", "origin_ip", "back_source_ip", "upstream"} {
		addrs = append(addrs, extractAddrsFromAttr(attrs, key)...)
	}
	// SLB/ALB 后端服务器
	for _, key := range []string{"backend_servers", "backend_server_list", "real_servers", "targets"} {
		addrs = append(addrs, extractAddrsFromAttr(attrs, key)...)
	}
	// 通用下游
	for _, key := range []string{"target_ip", "target_domain", "forward_to", "next_hop"} {
		addrs = append(addrs, extractAddrsFromAttr(attrs, key)...)
	}

	// EIP 绑定的实例 ID（如华为云 EIP 绑定 ELB，instance_id 是 ELB 的 UUID）
	if strings.Contains(inst.ModelUID, "eip") {
		if instID := getStr(attrs, "instance_id"); instID != "" {
			addrs = append(addrs, instID)
		}
	}

	return addrs
}

// extractAddrsFromAttr 从属性值中提取地址列表
// 支持 string、[]string、[]interface{}、primitive.A 等格式
func extractAddrsFromAttr(attrs map[string]interface{}, key string) []string {
	val, ok := attrs[key]
	if !ok || val == nil {
		return nil
	}
	var addrs []string
	switch v := val.(type) {
	case string:
		if v != "" {
			// 可能是逗号分隔的多个地址
			for _, s := range strings.Split(v, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					addrs = append(addrs, s)
				}
			}
		}
	case primitive.A:
		// MongoDB driver 的数组类型
		addrs = extractAddrsFromSlice([]interface{}(v))
	case []interface{}:
		addrs = extractAddrsFromSlice(v)
	}
	return addrs
}

// extractAddrsFromSlice 从 []interface{} 中提取地址
func extractAddrsFromSlice(items []interface{}) []string {
	var addrs []string
	for _, item := range items {
		switch it := item.(type) {
		case string:
			if it != "" {
				addrs = append(addrs, it)
			}
		case map[string]interface{}:
			addrs = append(addrs, extractAddrFromMap(it)...)
		case primitive.M:
			addrs = append(addrs, extractAddrFromMap(map[string]interface{}(it))...)
		}
	}
	return addrs
}

// extractAddrFromMap 从 map 中提取最佳匹配地址（只返回一个）
// 优先级：servername(ECS实例ID) > instanceid(ENI ID) > IP
func extractAddrFromMap(m map[string]interface{}) []string {
	// 1. servername 如 i-xxx 可以直接用 asset_id 匹配 ECS
	for _, key := range []string{"servername", "server_name"} {
		if v := getStr(m, key); v != "" {
			return []string{v}
		}
	}
	// 2. instanceid 如 eni-xxx 可以用 asset_id 匹配 ENI
	for _, key := range []string{"instanceid", "instance_id"} {
		if v := getStr(m, key); v != "" {
			return []string{v}
		}
	}
	// 3. IP 地址作为 fallback
	for _, key := range []string{"ip", "server_ip", "address"} {
		if v := getStr(m, key); v != "" {
			return []string{v}
		}
	}
	return nil
}

// matchByAddress 根据地址（IP 或域名）匹配 CMDB 实例
func matchByAddress(addr string, ipIndex map[string]*cmdbInstanceDoc, cnameIndex map[string]*cmdbInstanceDoc) *cmdbInstanceDoc {
	addr = strings.TrimRight(addr, ".")
	// 先按 IP 匹配
	if inst := ipIndex[addr]; inst != nil {
		return inst
	}
	// 再按域名/CNAME 精确匹配
	addrLower := strings.ToLower(addr)
	if inst := cnameIndex[addrLower]; inst != nil {
		return inst
	}
	// WAF/ALB CNAME 前缀匹配
	prefix := extractCDNDomainPrefix(addrLower)
	if prefix != "" {
		if inst := ipIndex[prefix]; inst != nil {
			if strings.Contains(inst.ModelUID, "waf") || strings.Contains(inst.ModelUID, "lb") {
				return inst
			}
		}
	}
	return nil
}

// matchInstance 根据 DNS 记录的 value 匹配 CMDB 实例
func (b *LiveTopologyBuilder) matchInstance(rec dnsRecordDoc, ipIndex map[string]*cmdbInstanceDoc, cnameIndex map[string]*cmdbInstanceDoc, cdnDomainIndex map[string][]*cmdbInstanceDoc) *cmdbInstanceDoc {
	value := strings.TrimRight(rec.Value, ".")
	valueLower := strings.ToLower(value)
	switch rec.Type {
	case "A", "AAAA":
		if inst := ipIndex[value]; inst != nil {
			return inst
		}
	case "CNAME":
		// 1. 精确匹配：CNAME value 完全等于某个实例的 cname 属性
		if inst := cnameIndex[valueLower]; inst != nil {
			return inst
		}
		// 2. CDN 前缀匹配：CNAME 格式通常是 "{domain_name}.{cdn_suffix}"
		prefix, provider := extractCDNDomainPrefixAndProvider(valueLower)
		if prefix != "" {
			// 2a. CDN 域名索引匹配
			if cdnInsts, ok := cdnDomainIndex[prefix]; ok {
				if provider != "" {
					for _, inst := range cdnInsts {
						if strings.Contains(inst.ModelUID, provider) {
							return inst
						}
					}
				}
				return nil
			}
			// 2b. ALB/WAF/SLB 前缀匹配（CNAME 直接指向 ALB/WAF 地址）
			if inst := ipIndex[prefix]; inst != nil {
				if strings.Contains(inst.ModelUID, "waf") || strings.Contains(inst.ModelUID, "lb") {
					return inst
				}
			}
		}
	}
	return nil
}

// extractCDNDomainPrefixAndProvider 从 CDN CNAME 中提取原始域名前缀和云厂商标识
// CDN CNAME 格式: {domain_name}.{cdn_provider_suffix}
// 例如: www.jlc-smt.com.c.vedcdnlb.com → ("www.jlc-smt.com", "volcengine")
//
//	www.jlc-smt.com.1eeb2723.cdnhwcurq03.com → ("www.jlc-smt.com", "huawei")
func extractCDNDomainPrefixAndProvider(cname string) (string, string) {
	// 已知 CDN 厂商 CNAME 后缀 → 云厂商标识
	type cdnSuffix struct {
		suffix   string
		provider string
	}
	cdnSuffixes := []cdnSuffix{
		// 火山引擎
		{".c.vedcdnlb.com", "volcengine"},
		{".cdn.volcenginecdn.com", "volcengine"},
		{".volccdn.com", "volcengine"},
		// 华为云
		{".cdnhwcurq03.com", "huawei"},
		{".cdnhwcurq02.com", "huawei"},
		{".cdnhwcurq01.com", "huawei"},
		{".cdnhwc2.com", "huawei"},
		{".cdnhwc1.com", "huawei"},
		// 阿里云
		{".kunlunaq.com", "aliyun"},
		{".kunlunca.com", "aliyun"},
		{".kunluncan.com", "aliyun"},
		{".kunlunsl.com", "aliyun"},
		{".kunlunpi.com", "aliyun"},
		{".alikunlun.com", "aliyun"},
		{".cdngslb.com", "aliyun"},
		{".alicdn.com", "aliyun"},
		// 阿里云 WAF
		{".c.yundunwaf1.com", "aliyun"},
		{".c.yundunwaf2.com", "aliyun"},
		{".c.yundunwaf3.com", "aliyun"},
		{".c.yundunwaf5.com", "aliyun"},
		{".c.yundunwaf.com", "aliyun"},
		{".yundunwaf1.com", "aliyun"},
		{".yundunwaf2.com", "aliyun"},
		{".yundunwaf3.com", "aliyun"},
		{".yundunwaf5.com", "aliyun"},
		{".yundunwaf.com", "aliyun"},
		// 华为云 WAF
		{".huaweicloudwaf.com", "huawei"},
		{".wafcloud3.com", "huawei"},
		// 腾讯云
		{".cdn.dnsv1.com", "tencent"},
		{".tdnsv5.com", "tencent"},
		{".tdnsv6.com", "tencent"},
		{".tdnsv8.com", "tencent"},
		// AWS
		{".cloudfront.net", "aws"},
		// 火山引擎 ALB/CLB — 格式: {id}.{region}.volcenginealb.com
		// region 如 cn-guangzhou 包含连字符，不是 hash，需要用更精确的后缀
		{".volcenginealb.com", "volcengine"},
		{".volcengineclb.com", "volcengine"},
		{".volcenginenlb.com", "volcengine"},
		// 阿里云 ALB/SLB
		{".alb.aliyuncsslb.com", "aliyun"},
		{".slb.aliyuncsslb.com", "aliyun"},
		{".aliyuncsslb.com", "aliyun"},
	}
	for _, s := range cdnSuffixes {
		if strings.HasSuffix(cname, s.suffix) {
			prefix := cname[:len(cname)-len(s.suffix)]
			// 去掉末尾可能的 hash 段（如 ".1eeb2723"）或地域段（如 ".cn-guangzhou"）
			for {
				lastDot := strings.LastIndex(prefix, ".")
				if lastDot <= 0 {
					break
				}
				segment := prefix[lastDot+1:]
				if looksLikeHash(segment) || looksLikeRegion(segment) {
					prefix = prefix[:lastDot]
				} else {
					break
				}
			}
			return prefix, s.provider
		}
	}
	// 通用策略：如果 CNAME 包含已知 CDN 关键词，尝试提取前缀
	cdnKeywords := []string{"cdn", "dcdn", "waf", "edgekey", "akamai", "fastly"}
	parts := strings.Split(cname, ".")
	for i := len(parts) - 1; i >= 2; i-- {
		for _, kw := range cdnKeywords {
			if strings.Contains(parts[i], kw) {
				candidate := strings.Join(parts[:i], ".")
				// 去掉末尾 hash/region 段
				for {
					lastDot := strings.LastIndex(candidate, ".")
					if lastDot <= 0 {
						break
					}
					segment := candidate[lastDot+1:]
					if looksLikeHash(segment) || looksLikeRegion(segment) {
						candidate = candidate[:lastDot]
					} else {
						break
					}
				}
				return candidate, ""
			}
		}
	}
	return "", ""
}

// extractCDNDomainPrefix 从 CDN/WAF CNAME 中提取原始域名前缀（不含云厂商信息）
func extractCDNDomainPrefix(cname string) string {
	prefix, _ := extractCDNDomainPrefixAndProvider(cname)
	return prefix
}

func looksLikeHash(s string) bool {
	if len(s) < 4 || len(s) > 20 {
		return false
	}
	// Hash 段通常是纯字母数字，不包含连字符（域名部分通常有连字符或更长）
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// looksLikeRegion 检查是否像云厂商地域标识（如 cn-guangzhou, ap-southeast-1, us-east-1）
func looksLikeRegion(s string) bool {
	regionPrefixes := []string{"cn-", "ap-", "us-", "eu-", "me-", "sa-", "af-"}
	sl := strings.ToLower(s)
	for _, p := range regionPrefixes {
		if strings.HasPrefix(sl, p) {
			return true
		}
	}
	return false
}

// queryDNSRecords 查询 DNS 记录（仅查询与业务链路相关的 A 和 CNAME 类型）
func (b *LiveTopologyBuilder) queryDNSRecords(ctx context.Context, tenantID, domainFilter string) ([]dnsRecordDoc, error) {
	query := bson.M{
		"tenant_id": tenantID,
		"type":      bson.M{"$in": bson.A{"A", "CNAME"}}, // 只查流量相关的记录类型，排除 MX/TXT/NS/SRV 等
	}
	if domainFilter != "" {
		parts := strings.SplitN(domainFilter, ".", 2)
		if len(parts) >= 2 {
			query["$or"] = bson.A{
				bson.M{"domain": domainFilter},
				bson.M{"domain": parts[1], "rr": parts[0]},
				bson.M{"domain": bson.M{"$regex": domainFilter, "$options": "i"}},
			}
		} else {
			query["$or"] = bson.A{
				bson.M{"domain": bson.M{"$regex": domainFilter, "$options": "i"}},
				bson.M{"rr": bson.M{"$regex": domainFilter, "$options": "i"}},
			}
		}
	}
	cursor, err := b.db.Collection("ecam_dns_record").Find(ctx, query, options.Find().SetLimit(200))
	if err != nil {
		return nil, fmt.Errorf("query dns records: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []dnsRecordDoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// queryAllInstances 查询租户下所有 CMDB 实例
// 优先加载拓扑相关的资源类型（CDN/WAF/LB/ECS/RDS 等），排除大量无关类型
// queryAllInstances — deprecated, use queryInstancesByModelUIDs
func (b *LiveTopologyBuilder) queryAllInstances(ctx context.Context, tenantID string) ([]cmdbInstanceDoc, error) {
	return b.queryInstancesByModelUIDs(ctx, tenantID, nil)
}

// queryInstancesByModelUIDs 按 model_uid 列表查询实例
func (b *LiveTopologyBuilder) queryInstancesByModelUIDs(ctx context.Context, tenantID string, modelUIDs []string) ([]cmdbInstanceDoc, error) {
	query := bson.M{"tenant_id": tenantID}
	if len(modelUIDs) > 0 {
		query["model_uid"] = bson.M{"$in": modelUIDs}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	opts := options.Find().SetLimit(10000)
	cursor, err := b.db.Collection("ecam_instance").Find(queryCtx, query, opts)
	if err != nil {
		return nil, fmt.Errorf("query instances: %w", err)
	}
	defer cursor.Close(queryCtx)
	var docs []cmdbInstanceDoc
	if err = cursor.All(queryCtx, &docs); err != nil {
		return nil, fmt.Errorf("decode instances: %w", err)
	}
	return docs, nil
}

// queryInstancesByDomain 按域名查询相关实例（CDN/WAF/LB 的 asset_id 或 cname 匹配）
func (b *LiveTopologyBuilder) queryInstancesByDomain(ctx context.Context, tenantID string, domainNames []string, cnameValues []string, aRecordIPs []string) ([]cmdbInstanceDoc, error) {
	if len(domainNames) == 0 && len(cnameValues) == 0 && len(aRecordIPs) == 0 {
		return nil, nil
	}
	// 构建查询条件：asset_id 匹配域名，或 cname 匹配 CNAME 值
	orConditions := bson.A{}
	// CDN/WAF 的 asset_id 通常是域名（如 www.jlc-dfm.com 或 www.jlc-dfm.com-waf）
	assetIDs := make([]string, 0)
	for _, dn := range domainNames {
		assetIDs = append(assetIDs, dn, dn+"-waf")
	}
	if len(assetIDs) > 0 {
		orConditions = append(orConditions, bson.M{"asset_id": bson.M{"$in": assetIDs}})
	}
	// CNAME 精确匹配（也用 asset_id 匹配 CNAME 前缀，如 ALB/ELB ID）
	if len(cnameValues) > 0 {
		orConditions = append(orConditions, bson.M{"attributes.cname": bson.M{"$in": cnameValues}})
		orConditions = append(orConditions, bson.M{"asset_id": bson.M{"$in": cnameValues}})
	}
	// domain_name 匹配
	if len(domainNames) > 0 {
		orConditions = append(orConditions, bson.M{"attributes.domain_name": bson.M{"$in": domainNames}})
	}
	// 华为 WAF 的 protected_hosts 包含域名
	if len(domainNames) > 0 {
		orConditions = append(orConditions, bson.M{"attributes.protected_hosts": bson.M{"$in": domainNames}})
	}
	// A 记录 IP 匹配 LB 的 VIP/address
	if len(aRecordIPs) > 0 {
		orConditions = append(orConditions, bson.M{"attributes.address": bson.M{"$in": aRecordIPs}})
		orConditions = append(orConditions, bson.M{"attributes.vip": bson.M{"$in": aRecordIPs}})
		orConditions = append(orConditions, bson.M{"attributes.public_ip": bson.M{"$in": aRecordIPs}})
		orConditions = append(orConditions, bson.M{"attributes.ip_address": bson.M{"$in": aRecordIPs}})
	}

	query := bson.M{
		"tenant_id": tenantID,
		"$or":       orConditions,
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor, err := b.db.Collection("ecam_instance").Find(queryCtx, query, options.Find().SetLimit(500))
	if err != nil {
		return nil, fmt.Errorf("query instances by domain: %w", err)
	}
	defer cursor.Close(queryCtx)
	var docs []cmdbInstanceDoc
	if err = cursor.All(queryCtx, &docs); err != nil {
		return nil, fmt.Errorf("decode instances: %w", err)
	}
	return docs, nil
}

// expandDownstream 从已加载的实例中提取下游地址，查询关联的 LB/ECS 实例
func (b *LiveTopologyBuilder) expandDownstream(ctx context.Context, tenantID string, instances []cmdbInstanceDoc) ([]cmdbInstanceDoc, error) {
	// 收集所有下游地址（WAF 的 source_ips、CDN 的 origins 等）
	downstreamAddrs := make(map[string]bool)
	for i := range instances {
		addrs := extractDownstreamAddresses(&instances[i])
		for _, a := range addrs {
			a = strings.TrimRight(a, ".")
			if a != "" {
				downstreamAddrs[a] = true
				// 也提取 CNAME 前缀（如 ALB ID）
				prefix := extractCDNDomainPrefix(strings.ToLower(a))
				if prefix != "" {
					downstreamAddrs[prefix] = true
				}
			}
		}
	}
	if len(downstreamAddrs) == 0 {
		return instances, nil
	}

	// 用下游地址查询关联实例
	addrList := make([]string, 0, len(downstreamAddrs))
	for a := range downstreamAddrs {
		addrList = append(addrList, a)
	}
	if len(addrList) > 200 {
		addrList = addrList[:200]
	}

	// 查询 asset_id 或 cname 或 address 匹配的实例
	query := bson.M{
		"tenant_id": tenantID,
		"$or": bson.A{
			bson.M{"asset_id": bson.M{"$in": addrList}},
			bson.M{"attributes.cname": bson.M{"$in": addrList}},
			bson.M{"attributes.domain_name": bson.M{"$in": addrList}},
			bson.M{"attributes.address": bson.M{"$in": addrList}},
			bson.M{"attributes.vip": bson.M{"$in": addrList}},
		},
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor, err := b.db.Collection("ecam_instance").Find(queryCtx, query, options.Find().SetLimit(500))
	if err != nil {
		return instances, fmt.Errorf("expand downstream: %w", err)
	}
	defer cursor.Close(queryCtx)
	var newDocs []cmdbInstanceDoc
	if err = cursor.All(queryCtx, &newDocs); err != nil {
		return instances, fmt.Errorf("decode downstream: %w", err)
	}

	// 合并，去重
	existingIDs := make(map[int64]bool)
	for _, inst := range instances {
		existingIDs[inst.ID] = true
	}
	for _, doc := range newDocs {
		if !existingIDs[doc.ID] {
			instances = append(instances, doc)
			existingIDs[doc.ID] = true
		}
	}

	// 第二轮：从所有 LB 实例中提取 backend_servers，查询 ECS/ENI
	// 包括初始查询和下游查询中的 LB 实例
	backendIPs := b.extractBackendIPsFromLBs(instances)
	if len(backendIPs) > 0 {
		backendInstances, ecsErr := b.queryInstancesByIPs(ctx, tenantID, backendIPs)
		if ecsErr == nil {
			// 从 ENI 实例中提取绑定的 ECS instance_id，继续查询 ECS
			var ecsIDs []string
			for _, doc := range backendInstances {
				if !existingIDs[doc.ID] {
					instances = append(instances, doc)
					existingIDs[doc.ID] = true
				}
				// ENI 透传：提取绑定的 ECS 实例 ID
				if strings.Contains(doc.ModelUID, "eni") {
					if ecsID := getStr(doc.Attributes, "instance_id"); ecsID != "" {
						ecsIDs = append(ecsIDs, ecsID)
					}
				}
			}
			// 查询 ENI 绑定的 ECS 实例
			if len(ecsIDs) > 0 {
				ecsInstances, _ := b.queryInstancesByIPs(ctx, tenantID, ecsIDs)
				for _, doc := range ecsInstances {
					if !existingIDs[doc.ID] {
						instances = append(instances, doc)
						existingIDs[doc.ID] = true
					}
				}
			}
		}
	}

	return instances, nil
}

// extractBackendIPsFromLBs 从 LB 实例的 backend_servers 中提取后端 IP 和实例 ID
func (b *LiveTopologyBuilder) extractBackendIPsFromLBs(instances []cmdbInstanceDoc) []string {
	seen := make(map[string]bool)
	var ips []string
	for _, inst := range instances {
		if !strings.Contains(inst.ModelUID, "lb") && !strings.Contains(inst.ModelUID, "slb") &&
			!strings.Contains(inst.ModelUID, "alb") && !strings.Contains(inst.ModelUID, "nlb") &&
			!strings.Contains(inst.ModelUID, "elb") && !strings.Contains(inst.ModelUID, "clb") {
			continue
		}
		addrs := extractDownstreamAddresses(&inst)
		for _, a := range addrs {
			if !seen[a] {
				seen[a] = true
				ips = append(ips, a)
			}
		}
	}
	return ips
}

// queryInstancesByIPs 按 IP/实例ID 查询 ECS/ENI 实例
func (b *LiveTopologyBuilder) queryInstancesByIPs(ctx context.Context, tenantID string, ips []string) ([]cmdbInstanceDoc, error) {
	if len(ips) == 0 {
		return nil, nil
	}
	if len(ips) > 100 {
		ips = ips[:100]
	}
	// 只用 asset_id 匹配（有索引，速度快）
	// ECS 的 asset_id = 实例ID（如 i-xxx），ENI 的 asset_id = eni-xxx
	// 后端服务器的 servername 通常就是 ECS 实例 ID
	query := bson.M{
		"tenant_id": tenantID,
		"asset_id":  bson.M{"$in": ips},
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cursor, err := b.db.Collection("ecam_instance").Find(queryCtx, query, options.Find().SetLimit(200))
	if err != nil {
		return nil, fmt.Errorf("query ecs by ip: %w", err)
	}
	defer cursor.Close(queryCtx)
	var docs []cmdbInstanceDoc
	if err = cursor.All(queryCtx, &docs); err != nil {
		return nil, fmt.Errorf("decode ecs: %w", err)
	}
	return docs, nil
}

// queryRelations 查询租户下所有 CMDB 实例关系
func (b *LiveTopologyBuilder) queryRelations(ctx context.Context, tenantID string) ([]cmdbRelationDoc, error) {
	cursor, err := b.db.Collection("ecam_instance_relation").Find(ctx, bson.M{"tenant_id": tenantID}, options.Find().SetLimit(10000))
	if err != nil {
		return nil, fmt.Errorf("query relations: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []cmdbRelationDoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// instanceToTopoNode 将 CMDB 实例转换为拓扑节点
func instanceToTopoNode(inst *cmdbInstanceDoc, now time.Time) domain.TopoNode {
	nodeType, category := modelUIDToType(inst.ModelUID)
	provider := extractProvider(inst.ModelUID)
	// 对 LB 类型，进一步区分 ALB/SLB/NLB/CLB
	if nodeType == domain.NodeTypeSLB {
		lbType := getStr(inst.Attributes, "load_balancer_type")
		if lbType == "" && strings.HasPrefix(inst.AssetID, "alb-") {
			lbType = "alb"
		} else if lbType == "" && strings.HasPrefix(inst.AssetID, "nlb-") {
			lbType = "nlb"
		} else if lbType == "" && strings.HasPrefix(inst.AssetID, "clb-") {
			lbType = "clb"
		}
		if lbType == "alb" {
			nodeType = "alb"
		} else if lbType == "nlb" {
			nodeType = "nlb"
		}
	}
	name := inst.AssetName
	if name == "" {
		name = inst.AssetID
	}
	return domain.TopoNode{
		ID: fmt.Sprintf("inst-%d", inst.ID), Name: name,
		Type: nodeType, Category: category, Provider: provider,
		Status: domain.StatusActive, SourceCollector: domain.SourceCloudAPI,
		TenantID: inst.TenantID, UpdatedAt: now,
		Attributes: inst.Attributes,
	}
}

// modelUIDToType 将 CMDB model_uid 映射为拓扑节点类型
func modelUIDToType(modelUID string) (nodeType, category string) {
	switch {
	case strings.Contains(modelUID, "cdn"):
		return domain.NodeTypeCDN, domain.CategoryNetwork
	case strings.Contains(modelUID, "waf"):
		return domain.NodeTypeWAF, domain.CategorySecurity
	case strings.Contains(modelUID, "slb"), strings.Contains(modelUID, "elb"),
		strings.Contains(modelUID, "alb"), strings.Contains(modelUID, "nlb"),
		strings.Contains(modelUID, "clb"), strings.HasSuffix(modelUID, "_lb"):
		return domain.NodeTypeSLB, domain.CategoryNetwork
	case strings.Contains(modelUID, "ecs"), strings.Contains(modelUID, "_vm"):
		return domain.NodeTypeECS, domain.CategoryCompute
	case strings.Contains(modelUID, "eni"):
		return "eni", domain.CategoryNetwork
	case strings.Contains(modelUID, "rds"):
		return domain.NodeTypeRDS, domain.CategoryDatabase
	case strings.Contains(modelUID, "redis"):
		return domain.NodeTypeRedis, domain.CategoryDatabase
	case strings.Contains(modelUID, "mongodb"):
		return "mongodb", domain.CategoryDatabase
	case strings.Contains(modelUID, "oss"), strings.Contains(modelUID, "s3"):
		return domain.NodeTypeOSS, domain.CategoryStorage
	case strings.Contains(modelUID, "vpc"):
		return "vpc", domain.CategoryNetwork
	case strings.Contains(modelUID, "eip"):
		return "eip", domain.CategoryNetwork
	case strings.Contains(modelUID, "nas"):
		return "nas", domain.CategoryStorage
	case strings.Contains(modelUID, "kafka"):
		return "kafka", domain.CategoryMiddleware
	case strings.Contains(modelUID, "elasticsearch"):
		return "elasticsearch", domain.CategoryMiddleware
	default:
		return domain.NodeTypeUnknown, domain.CategoryCompute
	}
}

// extractProvider 从 model_uid 提取云厂商
func extractProvider(modelUID string) string {
	switch {
	case strings.HasPrefix(modelUID, "aliyun_"):
		return domain.ProviderAliyun
	case strings.HasPrefix(modelUID, "aws_"):
		return domain.ProviderAWS
	case strings.HasPrefix(modelUID, "tencent_"):
		return domain.ProviderTencent
	case strings.HasPrefix(modelUID, "huawei_"):
		return domain.ProviderHuawei
	case strings.HasPrefix(modelUID, "volcano_"), strings.HasPrefix(modelUID, "volcengine_"):
		return domain.ProviderVolcano
	default:
		return ""
	}
}

func getStr(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	v, ok := attrs[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", s)
	}
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return strings.TrimRight(s, "-")
}

func countType(nodes []domain.TopoNode, t string) int {
	c := 0
	for _, n := range nodes {
		if n.Type == t {
			c++
		}
	}
	return c
}

func maxDepthOf(nodes []domain.TopoNode) int {
	m := 0
	for _, n := range nodes {
		if n.DagDepth > m {
			m = n.DagDepth
		}
	}
	return m
}
