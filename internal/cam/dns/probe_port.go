package dns

import (
	"context"
	"strings"
)

// RecordReadPort 探测目标只读端口（cert probe 模块消费）：枚举有 DNS 记录的
// 租户 + 按租户批量返回 TLS-relevant 记录导出的拨测目标（A/AAAA/CNAME），
// linked_resource 由内部 linker 现算填好。
//
// 设计：cert 模块只依赖此接口，不碰 DnsRecordDAO/ResourceLinker，模块边界清晰。
// 将来 DNS 拆成独立微服务时，把实现从"DAO+linker"换成 gRPC client，cert 零改动。
type RecordReadPort interface {
	// ListTenantsWithRecords 返回有解析记录的全部 tenantID（去重）。
	// 本服务无本地租户注册表（租户由 eiam 管理），从 DNS 记录 distinct tenant_id
	// 取覆盖面——无 DNS 记录的租户无可拨测目标，自然不入轮。
	ListTenantsWithRecords(ctx context.Context) ([]int64, error)
	// ListProbeTargets 批量返回某租户的可拨测子域名（A/AAAA/CNAME 记录导出）。
	ListProbeTargets(ctx context.Context, tenantID int64) ([]ProbeTarget, error)
}

// ProbeTarget 一条 DNS 记录导出的拨测目标（子域名 + 链路关联资源）。
type ProbeTarget struct {
	Hostname       string // 全限定子域名（rr+domain；@→根域；通配符记录跳过不导出）
	RecordType     string // A/AAAA/CNAME
	TenantID       int64
	LinkedResource *LinkedResource // cdn/waf/external（linker 现算）；nil=未识别
}

// recordReadPort RecordReadPort 生产实现：DAO 批量读 + linker 现算 + hostname 拼装。
type recordReadPort struct {
	dao    *DnsRecordDAO
	linker *ResourceLinker
}

// NewRecordReadPort 创建探测目标只读端口（经 dns 模块装配层注入 cert probe）。
func NewRecordReadPort(dao *DnsRecordDAO) RecordReadPort {
	return &recordReadPort{dao: dao, linker: NewResourceLinker()}
}

func (r *recordReadPort) ListTenantsWithRecords(ctx context.Context) ([]int64, error) {
	return r.dao.DistinctTenantIDs(ctx)
}

func (r *recordReadPort) ListProbeTargets(ctx context.Context, tenantID int64) ([]ProbeTarget, error) {
	docs, err := r.dao.ListProbeRecordsByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]ProbeTarget, 0, len(docs))
	for _, d := range docs {
		if d.Status != "" && !strings.EqualFold(d.Status, "enable") && !strings.EqualFold(d.Status, "enabled") {
			continue // 禁用记录不拨测
		}
		hostname := buildProbeHostname(d.RR, d.Domain)
		if hostname == "" {
			continue // 通配符记录（*.x / *）无法直接 SNI 拨测，跳过
		}
		out = append(out, ProbeTarget{
			Hostname:       hostname,
			RecordType:     d.Type,
			TenantID:       d.TenantID,
			LinkedResource: r.linker.Identify(d.Type, d.Value),
		})
	}
	return out, nil
}

// buildProbeHostname 由 rr + domain 拼全限定子域名：
//   - rr="@" 或空 → 根域 domain 本身
//   - rr 含 "*"（通配符记录）→ 空串（调用方跳过；通配符无法直接 SNI 拨测）
//   - 其余 → rr + "." + domain
func buildProbeHostname(rr, domain string) string {
	rr = strings.TrimSpace(rr)
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	if rr == "" || rr == "@" {
		return domain
	}
	if strings.Contains(rr, "*") {
		return "" // 通配符 DNS 记录跳过（与 cert probe 的 wildcard_skipped 语义一致）
	}
	if strings.HasPrefix(rr, "_") {
		return "" // 下划线前缀协议记录（CA 域名校验/SPF/DKIM 等）非 TLS 端点，跳过
	}
	return rr + "." + domain
}
