package ioc

import (
	"context"

	auditdomain "github.com/Havens-blog/e-cam-service/internal/audit/domain"
	auditservice "github.com/Havens-blog/e-cam-service/internal/audit/service"
	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// 本文件为 cert 变更单审计的 audit 侧适配（cert 服务抽取 · 出站解耦）：
// cert 审计桥依赖 cert 中性端口 service.ChangeAuditStore 与中性条目类型，不再
// import internal/audit 的 domain/service；此 adapter 用 internal/audit
// ChangeOrderAuditService 满足该端口（条目字段逐一翻译，单集合仅追加语义不变），
// 审计数据归属推迟到接口背后。

// certAuditStore 把 cert ChangeAuditStore 适配到 internal/audit 审计服务。
type certAuditStore struct {
	svc *auditservice.ChangeOrderAuditService
}

func (a certAuditStore) Record(ctx context.Context, e certservice.ChangeAuditEntry) error {
	return a.svc.Record(ctx, toAuditEntry(e))
}

func (a certAuditStore) RecordDedup(ctx context.Context, e certservice.ChangeAuditEntry) (bool, error) {
	return a.svc.RecordDedup(ctx, toAuditEntry(e))
}

func (a certAuditStore) ListByOrder(ctx context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	entries, err := a.svc.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return fromAuditEntries(entries), nil
}

func (a certAuditStore) ListOrphanCleanupResults(ctx context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	entries, err := a.svc.ListOrphanCleanupResults(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return fromAuditEntries(entries), nil
}

func (a certAuditStore) ListUnmetDomains(ctx context.Context, orderID string) ([]string, error) {
	return a.svc.ListUnmetDomains(ctx, orderID)
}

// toAuditEntry cert 中性条目 → internal/audit 领域条目。
func toAuditEntry(e certservice.ChangeAuditEntry) auditdomain.ChangeOrderAuditEntry {
	return auditdomain.ChangeOrderAuditEntry{
		OrderID:      e.OrderID,
		ItemID:       e.ItemID,
		Actor:        e.Actor,
		Action:       e.Action,
		Detail:       e.Detail,
		At:           e.At,
		Cloud:        e.Cloud,
		CloudCertID:  e.CloudCertID,
		OrphanAction: e.OrphanAction,
		Success:      e.Success,
		UnmetDomains: e.UnmetDomains,
		DedupKey:     e.DedupKey,
	}
}

// fromAuditEntries internal/audit 领域条目 → cert 中性条目。
func fromAuditEntries(src []auditdomain.ChangeOrderAuditEntry) []certservice.ChangeAuditEntry {
	out := make([]certservice.ChangeAuditEntry, len(src))
	for i, e := range src {
		out[i] = certservice.ChangeAuditEntry{
			OrderID:      e.OrderID,
			ItemID:       e.ItemID,
			Actor:        e.Actor,
			Action:       e.Action,
			Detail:       e.Detail,
			At:           e.At,
			Cloud:        e.Cloud,
			CloudCertID:  e.CloudCertID,
			OrphanAction: e.OrphanAction,
			Success:      e.Success,
			UnmetDomains: e.UnmetDomains,
			DedupKey:     e.DedupKey,
		}
	}
	return out
}
