package service

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------
// 按单审计写入端口与公共口径（任务 7.2，EIAM 权限资源同步与审计接入）：
//
// 写入侧统一载荷 ChangeAuditEvent + 端口 ChangeAuditWriter（web 层订单
// 生命周期事件与执行引擎 item_result）；5.8 RollbackAuditRecorder、
// 5.9 OrphanCleanupRecorder、5.10 VerifyWindowRecorder 既有端口不变，
// 生产实现由模块装配层 changeAuditBridge 统一接线 internal/audit（仅追加，
// Hard Rule：无任何 update/delete 审计代码路径；审计失败不阻塞业务主流程）。
// 读侧端口见 change_query_service.go（ChangeAuditSource 等，7.2 同桥接线）。
// ---------------------------------------------------------------------

// AuditAction 按单审计 action 取值（api-handbook 变更审计契约；cancel 为
// AC 全量审计口径的补充订单级事件，action 清单"覆盖"而非"穷尽"）。
const (
	// AuditActionCreate 生成变更清单。
	AuditActionCreate = "create"
	// AuditActionConfirm 确认执行/人工续批。
	AuditActionConfirm = "confirm"
	// AuditActionExecute 触发批量执行。
	AuditActionExecute = "execute"
	// AuditActionCancel 取消变更单。
	AuditActionCancel = "cancel"
	// AuditActionItemResult 变更项执行终态（executor 系统事件）。
	AuditActionItemResult = "item_result"
	// AuditActionRollback 回滚事件（5.8 RollbackAuditRecorder 产出）。
	AuditActionRollback = "rollback"
	// AuditActionVerify 验证窗口关闭未达标存档（5.10）。
	AuditActionVerify = "verify"
	// AuditActionOrphanCleanup 孤儿证书清理结果（5.9）。
	AuditActionOrphanCleanup = "orphan_cleanup"
)

// 系统事件 actor 标识（ChangeAuditLog.Actor 口径：系统事件=scheduler/
// executor 标识；HTTP 触发路径经 ctx 携带操作者时优先用操作者）。
const (
	// ActorExecutor 执行引擎事件（item_result）。
	ActorExecutor = "executor"
	// ActorScheduler 调度器事件（verify/orphan_cleanup 消费）。
	ActorScheduler = "scheduler"
)

// ChangeAuditEvent 按单审计事件统一写入载荷（web/service 写入点 → 端口）。
// Detail 为静态文案+安全参数（资源定位/云证书 ID/错误码），不含私钥/凭证
// 片段；At 零值由实现方补当前时间。
type ChangeAuditEvent struct {
	OrderID string    // 关联变更单
	ItemID  string    // 项级事件必填；订单级为空
	Actor   string    // 人工操作=EIAM 账号；系统事件=executor/scheduler
	Action  string    // AuditAction* 常量
	Detail  string    // 机器可读详情
	At      time.Time // 事件时间
}

// ChangeAuditWriter 按单审计流水写入端口（7.2 生产实现 internal/audit，
// 装配层 changeAuditBridge；nil=no-op）。仅追加；写入失败不阻塞调用方
// 主流程（错误返回由调用方决定吞没/聚合，桥实现负责日志告警）。
type ChangeAuditWriter interface {
	// WriteChangeAudit 追加一条订单审计事件。
	WriteChangeAudit(ctx context.Context, e ChangeAuditEvent) error
}

// operatorCtxKey ctx 内操作者键（web 角色中间件注入，审计写入点读取；
// 未携带时系统事件回退 executor/scheduler 标识）。
type operatorCtxKey struct{}

// WithOperator 将操作者（EIAM 账号）写入 ctx：web 层中间件统一注入，
// service→recorder 链路透传（人工触发的系统路径事件仍可归因操作者）。
func WithOperator(ctx context.Context, operator string) context.Context {
	return context.WithValue(ctx, operatorCtxKey{}, operator)
}

// OperatorFromContext 读取 ctx 操作者；未携带返回空串。
func OperatorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(operatorCtxKey{}).(string); ok {
		return v
	}
	return ""
}
