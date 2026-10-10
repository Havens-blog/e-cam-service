package service

import "context"

// 本文件声明 cert 域变更单审计的中性存储端口（消费方接口）。
//
// cert 的审计桥（internal/cert.changeAuditBridge）据此依赖本端口与 cert 自有的
// 中性条目类型 ChangeAuditEntry，不再 import internal/audit 的 domain/service——
// 为 cert 服务抽取清除出站耦合。生产实现由组合根用 internal/audit
// ChangeOrderAuditService 适配（单集合仅追加；字段逐一翻译），审计数据归属推迟
// 到接口背后：cert 抽为独立服务时换实现即可，不改桥与端口。

// ChangeAuditEntry 变更单审计条目（cert 侧中性投影，等价
// internal/audit domain.ChangeOrderAuditEntry 的字段集；At 为 Unix 毫秒）。
type ChangeAuditEntry struct {
	OrderID string
	ItemID  string
	Actor   string
	Action  string
	Detail  string
	At      int64

	// 孤儿清理载荷（action=orphan_cleanup 事件附加）。
	Cloud        string
	CloudCertID  string
	OrphanAction string
	Success      *bool

	// 验证窗口载荷（action=verify 事件附加）。
	UnmetDomains []string

	// DedupKey 幂等去重键（orphan:(cloudCertID,action,success)；verify:at）；
	// 空=普通事件不参与去重。
	DedupKey string
}

// ChangeAuditStore 变更单审计存储端口（单集合仅追加，无 update/delete 路径）。
// 方法集对齐 internal/audit ChangeOrderAuditService 的审计写入/查询面。
type ChangeAuditStore interface {
	// Record 追加审计条目。
	Record(ctx context.Context, entry ChangeAuditEntry) error
	// RecordDedup 幂等追加（去重键命中返回 inserted=false）。
	RecordDedup(ctx context.Context, entry ChangeAuditEntry) (bool, error)
	// ListByOrder 按单号查询审计流水（at 升序）。
	ListByOrder(ctx context.Context, orderID string) ([]ChangeAuditEntry, error)
	// ListOrphanCleanupResults 按单查询孤儿清理结果条目（at 升序）。
	ListOrphanCleanupResults(ctx context.Context, orderID string) ([]ChangeAuditEntry, error)
	// ListUnmetDomains 窗口关闭未达标域名清单（最近一条非空存档）。
	ListUnmetDomains(ctx context.Context, orderID string) ([]string, error)
}
