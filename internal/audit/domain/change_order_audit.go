package domain

// ChangeOrderAuditEntry 变更单维度审计流水（任务 7.2，cert 域变更全量审计）。
//
// 仅追加（append-only，Hard Rule）：DAO 只暴露 Append/查询，不存在任何
// update/delete 代码路径；审计失败不阻塞业务主流程（调用方记日志）。
//
// 单集合多投影：全部事件可按单号查询（ListByOrder → {at,actor,action,
// detail,itemId?} 契约）；orphan_cleanup/verify 两类事件附带结构化载荷，
// 同时承载 5.9/5.10 报告存档读端口（ChangeReport.OrphanCleanup /
// UnmetDomains 权威来源），DedupKey 支撑幂等去重（唯一稀疏索引）。
type ChangeOrderAuditEntry struct {
	ID      int64  `json:"id" bson:"id"`
	OrderID string `json:"order_id" bson:"order_id"`                   // 关联变更单号
	Actor   string `json:"actor" bson:"actor"`                         // 人工操作=EIAM 账号；系统事件=executor/scheduler 标识
	Action  string `json:"action" bson:"action"`                       // create|confirm|execute|cancel|item_result|rollback|verify|orphan_cleanup
	Detail  string `json:"detail" bson:"detail"`                       // 机器可读详情（静态文案+安全参数，不含私钥/凭证片段）
	ItemID  string `json:"item_id,omitempty" bson:"item_id,omitempty"` // 项级事件必填；订单级为空
	At      int64  `json:"at" bson:"at"`                               // 事件时间（Unix 毫秒）

	// 孤儿清理载荷（action=orphan_cleanup 事件附加，5.9 报告存档投影）。
	Cloud        string `json:"cloud,omitempty" bson:"cloud,omitempty"`
	CloudCertID  string `json:"cloud_cert_id,omitempty" bson:"cloud_cert_id,omitempty"`
	OrphanAction string `json:"orphan_action,omitempty" bson:"orphan_action,omitempty"` // cleanup|skip_keep
	Success      *bool  `json:"success,omitempty" bson:"success,omitempty"`

	// 验证窗口载荷（action=verify 事件附加，5.10 未达标清单存档投影）。
	UnmetDomains []string `json:"unmet_domains,omitempty" bson:"unmet_domains,omitempty"`

	// DedupKey 幂等去重键（orphan：(cloudCertID,action,success)；verify：at）；
	// 空=普通事件不参与去重（唯一稀疏索引仅覆盖非空键）。
	DedupKey string `json:"dedup_key,omitempty" bson:"dedup_key,omitempty"`
}
