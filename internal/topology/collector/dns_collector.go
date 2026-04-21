package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
)

// 已知 CDN 域名后缀（用于 CNAME 目标识别）
var cdnDomainSuffixes = []string{
	".cdn.aliyuncs.com", ".alicdn.com", ".kunlunaq.com",
	".cloudfront.net",
	".cdn.myqcloud.com",
	".cdn.volccdn.com",
	".cdn.hwcloudcdn.cn",
}

// 已知 WAF 域名后缀
var wafDomainSuffixes = []string{
	".yundunwaf.com", ".aliyunwaf.com",
	".awswaf.com",
	".waf.tencentcloudapi.com",
}

// 已知 OSS/S3 域名后缀
var ossDomainSuffixes = []string{
	".oss-", ".aliyuncs.com",
	".s3.amazonaws.com", ".s3-",
	".cos.", ".myqcloud.com",
	".obs.", ".myhuaweicloud.com",
}

// DNSRecord DNS 解析记录（从云 API 采集的原始数据）
type DNSRecord struct {
	Domain     string // 域名
	RecordType string // A / CNAME / AAAA
	Value      string // 解析值
	Provider   string // DNS 服务商
	TTL        int    // TTL
}

// DNSProvider DNS 服务商接口（由各云厂商 SDK 实现）
type DNSProvider interface {
	// ListRecords 获取指定域名的所有解析记录
	ListRecords(ctx context.Context, domainName string) ([]DNSRecord, error)
	// ListDomains 获取所有托管域名
	ListDomains(ctx context.Context) ([]string, error)
	// Provider 云厂商标识
	Provider() string
}

// DNSCollector DNS 记录采集器
type DNSCollector struct {
	providers []DNSProvider
}

// NewDNSCollector 创建 DNS 采集器
func NewDNSCollector(providers ...DNSProvider) *DNSCollector {
	return &DNSCollector{providers: providers}
}

func (c *DNSCollector) Name() string { return "dns_collector" }

// Collect 采集所有 DNS 记录并转换为拓扑节点和边
func (c *DNSCollector) Collect(ctx context.Context, tenantID string) ([]domain.TopoNode, []domain.TopoEdge, error) {
	nodes := make([]domain.TopoNode, 0)
	edges := make([]domain.TopoEdge, 0)

	for _, provider := range c.providers {
		domains, err := provider.ListDomains(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list domains from %s: %w", provider.Provider(), err)
		}

		for _, d := range domains {
			records, err := provider.ListRecords(ctx, d)
			if err != nil {
				continue // 单个域名失败不影响其他
			}

			for _, rec := range records {
				if rec.RecordType != "A" && rec.RecordType != "CNAME" {
					continue // 只处理 A 和 CNAME 记录
				}

				// 创建 DNS 入口节点
				nodeID := fmt.Sprintf("dns-%s", rec.Domain)
				dnsNode := domain.TopoNode{
					ID:              nodeID,
					Name:            rec.Domain,
					Type:            domain.NodeTypeDNSRecord,
					Category:        domain.CategoryDNS,
					Provider:        provider.Provider(),
					SourceCollector: domain.SourceDNSAPI,
					Status:          domain.StatusActive,
					Attributes: map[string]interface{}{
						"record_type":  rec.RecordType,
						"record_value": rec.Value,
						"ttl":          rec.TTL,
					},
					TenantID:  tenantID,
					UpdatedAt: time.Now(),
				}
				nodes = append(nodes, dnsNode)

				// 解析第一跳目标
				targetID, targetType := c.resolveFirstHop(rec)
				if targetID != "" {
					edge := domain.TopoEdge{
						ID:              fmt.Sprintf("e-%s-%s", nodeID, targetID),
						SourceID:        nodeID,
						TargetID:        targetID,
						Relation:        domain.RelationResolve,
						Direction:       domain.DirectionOutbound,
						SourceCollector: domain.SourceDNSAPI,
						Status:          domain.EdgeStatusActive,
						TenantID:        tenantID,
						UpdatedAt:       time.Now(),
					}
					edges = append(edges, edge)

					// 如果目标是 external 类型，创建 external 节点
					if targetType == domain.NodeTypeExternal {
						extNode := domain.TopoNode{
							ID:              targetID,
							Name:            rec.Value,
							Type:            domain.NodeTypeExternal,
							Category:        domain.CategoryNetwork,
							SourceCollector: domain.SourceDNSAPI,
							Status:          domain.StatusActive,
							TenantID:        tenantID,
							UpdatedAt:       time.Now(),
						}
						nodes = append(nodes, extNode)
					}
				}
			}
		}
	}

	return nodes, edges, nil
}

// resolveFirstHop 根据 DNS 记录解析第一跳目标节点 ID 和类型
// 返回 (targetNodeID, targetNodeType)
func (c *DNSCollector) resolveFirstHop(rec DNSRecord) (string, string) {
	value := strings.ToLower(rec.Value)

	if rec.RecordType == "CNAME" {
		// CNAME → CDN
		if matchesSuffix(value, cdnDomainSuffixes) {
			return fmt.Sprintf("cdn-%s", sanitizeID(rec.Value)), domain.NodeTypeCDN
		}
		// CNAME → WAF
		if matchesSuffix(value, wafDomainSuffixes) {
			return fmt.Sprintf("waf-%s", sanitizeID(rec.Value)), domain.NodeTypeWAF
		}
		// CNAME → OSS/S3
		if matchesSuffix(value, ossDomainSuffixes) {
			return fmt.Sprintf("oss-%s", sanitizeID(rec.Value)), domain.NodeTypeOSS
		}
		// 无法识别的 CNAME → external
		return fmt.Sprintf("ext-%s", sanitizeID(rec.Value)), domain.NodeTypeExternal
	}

	if rec.RecordType == "A" {
		// A 记录 → 可能是 SLB VIP 或 ECS EIP
		// 这里返回 IP 作为临时 ID，后续由 CloudResCollector 关联到具体资源
		return fmt.Sprintf("ip-%s", rec.Value), domain.NodeTypeUnknown
	}

	return "", ""
}

// matchesSuffix 检查域名是否匹配任一后缀
func matchesSuffix(domain string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.Contains(domain, suffix) {
			return true
		}
	}
	return false
}

// sanitizeID 将域名转换为合法的节点 ID（替换特殊字符）
func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.TrimRight(s, "-")
	return s
}
