package service

// ---------------------------------------------------------------------
// 告警通道扩展类型（任务 4.3；对接 internal/alert 证书域通道适配器）
// ---------------------------------------------------------------------

// AlertCategoryOps 运维处置类通知（任务 4.3）：孤儿清理失败、批间暂停超时取消、
// 执行心跳超时等 scheduler 处置通知。非 PRD 四类业务告警
// （expiry/tls_diff/change_linked/rollback_failed）——通道按常规路由触达，
// 计数与统计口径不计入四类业务告警。
const AlertCategoryOps AlertCategory = "ops"

// VerifyWindowContext 验证窗口告警路由判定入参（任务 4.3）。
//
// 由事件发布方（5.10 验证窗口服务）填充；internal/alert 通道按本上下文做
// change_linked 专用路由判定：
//   - Active=true 且 AlertConfig.verifyWindowRoute.enabled=true → 走专用通道
//     （webhookUrls/emailGroup，payload 附 orderId/预期指纹/达标计数）
//   - Active=true 且 enabled=false → 复用常规通道 + changeLinked 标记
//   - Active=false（窗口已关闭）→ 恢复常规路由（不带变更关联专用语义）
//
// nil 视同 Active=true（change_linked 事件仅在验证窗口内产生，4.1/5.10 语义）。
type VerifyWindowContext struct {
	Active              bool   // 窗口是否开启（5.10 控制；关闭后通道恢复常规路由）
	OrderID             string // 关联变更单号（冗余于 CertAlertEvent.OrderID，窗口上下文权威值）
	ExpectedFingerprint string // 预期终态指纹（verifyExpected.newCertFingerprint 快照）
	PassCount           int    // 达标计数（窗口内连续一致探测次数，verifyConfirmProbes 判据）
}
