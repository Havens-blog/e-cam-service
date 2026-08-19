package domain

import "fmt"

// 变更单 9 态状态机（任务 5.1）：合法迁移白名单 + Cancel 语义分类。
// 并发正确性（activeMutex 互斥、终态 token 清除）由仓储层原子原语与
// uk_active_mutex 部分唯一索引强制（见 ChangeOrderRepository），本文件
// 只承载纯状态机判定，不触达存储。

// changeTransitions 合法迁移白名单（PRD 状态机 + tech-design 分批批间循环）：
//
//	draft → pending_confirm | cancelled（人工取消）
//	pending_confirm → executing | cancelled
//	executing → verifying | cancelled（cancelled 为中止路径终态：running 项
//	  完成后按剩余项重算收敛，见 CancelModeAbort）
//	verifying → executing（分批单批间循环，activeMutex 全程持有）
//	          | completed | partial_completed
//	completed / partial_completed → rolled_back | rollback_failed（回滚入口 5.8）
//	rolled_back / rollback_failed / cancelled 为终态，无出边
//
// 回滚"入口"在执行中（失败项后）/验证中/部分完成均可用（PRD）指发起时机；
// rolled_back/rollback_failed 终态迁移仅自 completed/partial_completed 发生
// （tech-design Interface 3 与任务 5.1 AC 白名单）。
var changeTransitions = map[ChangeStatus][]ChangeStatus{
	ChangeStatusDraft:            {ChangeStatusPendingConfirm, ChangeStatusCancelled},
	ChangeStatusPendingConfirm:   {ChangeStatusExecuting, ChangeStatusCancelled},
	ChangeStatusExecuting:        {ChangeStatusVerifying, ChangeStatusCancelled},
	ChangeStatusVerifying:        {ChangeStatusExecuting, ChangeStatusCompleted, ChangeStatusPartialCompleted},
	ChangeStatusCompleted:        {ChangeStatusRolledBack, ChangeStatusRollbackFailed},
	ChangeStatusPartialCompleted: {ChangeStatusRolledBack, ChangeStatusRollbackFailed},
}

// InvalidTransitionError 状态机白名单外的非法迁移（调用方误用或状态漂移）。
// From/To 为状态枚举字符串，不含敏感材料。
type InvalidTransitionError struct {
	From ChangeStatus
	To   ChangeStatus
}

// Error 实现 error；消息统一 "cert: " 前缀（与 CertError/仓储哨兵风格一致）。
func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("cert: illegal change order transition %s -> %s", e.From, e.To)
}

// Transition 校验变更单状态迁移合法性：白名单内返回 nil，
// 白名单外（含自迁移、终态出边、未知状态）返回 *InvalidTransitionError。
func Transition(current, target ChangeStatus) error {
	for _, to := range changeTransitions[current] {
		if to == target {
			return nil
		}
	}
	return &InvalidTransitionError{From: current, To: target}
}

// IsTerminalChangeStatus 终态判定：进入终态时须经仓储原子原语
// 同一 update 清除 activeMutex（防终态单残留 token 阻塞新单）。
func IsTerminalChangeStatus(s ChangeStatus) bool {
	for _, v := range TerminalChangeStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// CancelMode 取消路径分类（任务 5.1 AC：Cancel 语义三分支）。
type CancelMode int

const (
	// CancelModeNone 不可取消：verifying 与全部终态 → 409 CHANGE_NOT_CANCELLABLE。
	CancelModeNone CancelMode = iota
	// CancelModeWhole 整单取消：draft/pending_confirm/executing+批间暂停
	//（batchInfo.paused=true）→ 未执行项标 skipped、转 cancelled 终态并
	// 同一原子 update 清除 activeMutex。
	CancelModeWhole
	// CancelModeAbort 执行中止：仅 pending 项标 skipped，running 项不中断，
	// 订单保持 executing；待运行项完成后按剩余项重算收敛 cancelled（5.5/5.7）。
	CancelModeAbort
)

// CancelModeOf 按当前状态与分批信息判定取消路径（纯函数，供 Cancel 与
// 超时取消调度复用）。paused 仅在 executing 态构成整单取消条件；
// verifying 即便残留暂停标记也不可取消���
func CancelModeOf(status ChangeStatus, batch *BatchInfo) CancelMode {
	switch status {
	case ChangeStatusDraft, ChangeStatusPendingConfirm:
		return CancelModeWhole
	case ChangeStatusExecuting:
		if batch != nil && batch.Paused {
			return CancelModeWhole
		}
		return CancelModeAbort
	default: // verifying 与全部终态
		return CancelModeNone
	}
}
