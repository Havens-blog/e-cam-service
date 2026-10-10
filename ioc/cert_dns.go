package ioc

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/dns"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// 本文件为 cert probe 的 cam/dns 侧适配（cert 服务抽取 · 出站解耦）：
// cert 的 DNSRecordSource 端口以 cert 自有投影类型（DNSProbeTarget/
// DNSLinkedResource）表达，不再依赖 cam/dns 的领域类型；此 adapter 将
// cam/dns.RecordReadPort 的返回逐条翻译为 cert 投影，使 cert 不再 import
// internal/cam/dns。

// certDNSRecordSource 把 cam/dns.RecordReadPort 适配到 cert DNSRecordSource。
type certDNSRecordSource struct {
	port dns.RecordReadPort
}

func (s certDNSRecordSource) ListTenantsWithRecords(ctx context.Context) ([]int64, error) {
	return s.port.ListTenantsWithRecords(ctx)
}

func (s certDNSRecordSource) ListProbeTargets(ctx context.Context, tenantID int64) ([]certservice.DNSProbeTarget, error) {
	src, err := s.port.ListProbeTargets(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]certservice.DNSProbeTarget, len(src))
	for i, t := range src {
		out[i] = certservice.DNSProbeTarget{
			Hostname:       t.Hostname,
			RecordType:     t.RecordType,
			RecordValue:    t.RecordValue,
			TenantID:       t.TenantID,
			LinkedResource: translateLinkedResource(t.LinkedResource),
		}
	}
	return out, nil
}

// translateLinkedResource 翻译链路关联资源指针（nil 透传）。
func translateLinkedResource(lr *dns.LinkedResource) *certservice.DNSLinkedResource {
	if lr == nil {
		return nil
	}
	return &certservice.DNSLinkedResource{
		Type: lr.Type,
		Name: lr.Name,
		ID:   lr.ID,
	}
}
