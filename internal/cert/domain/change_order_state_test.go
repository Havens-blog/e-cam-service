package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legalChangeTransitionPairs 合法迁移全集（AC-1 白名单的独立复述，
// 与实现 changeTransitions 分离编写以形成真实校验）：
//   - 主链：draft→pending_confirm→executing↔verifying→completed/partial_completed
//   - 回滚终链：completed/partial_completed→rolled_back/rollback_failed
//   - 取消来源：draft/pending_confirm/executing→cancelled（executing 侧为
//     中止路径完成后重算收敛的终态；verifying 不可取消）
var legalChangeTransitionPairs = []struct {
	from, to ChangeStatus
}{
	{ChangeStatusDraft, ChangeStatusPendingConfirm},
	{ChangeStatusDraft, ChangeStatusCancelled},
	{ChangeStatusPendingConfirm, ChangeStatusExecuting},
	{ChangeStatusPendingConfirm, ChangeStatusCancelled},
	{ChangeStatusExecuting, ChangeStatusVerifying},
	{ChangeStatusExecuting, ChangeStatusCancelled},
	{ChangeStatusVerifying, ChangeStatusExecuting},
	{ChangeStatusVerifying, ChangeStatusCompleted},
	{ChangeStatusVerifying, ChangeStatusPartialCompleted},
	{ChangeStatusCompleted, ChangeStatusRolledBack},
	{ChangeStatusCompleted, ChangeStatusRollbackFailed},
	{ChangeStatusPartialCompleted, ChangeStatusRolledBack},
	{ChangeStatusPartialCompleted, ChangeStatusRollbackFailed},
}

// TestChangeTransitionMatrix 全量 9×9 迁移矩阵：白名单外一律拒绝
// （含自迁移、终态出边、跨级跳跃如 draft→executing、verifying→cancelled）。
func TestChangeTransitionMatrix(t *testing.T) {
	all := append(append([]ChangeStatus{}, ActiveChangeStatuses...), TerminalChangeStatuses...)
	require.Len(t, all, 9, "9 态基数（活跃+终态划分，任务 1.2）")

	legal := map[ChangeStatus]map[ChangeStatus]bool{}
	for _, p := range legalChangeTransitionPairs {
		if legal[p.from] == nil {
			legal[p.from] = map[ChangeStatus]bool{}
		}
		legal[p.from][p.to] = true
	}

	for _, from := range all {
		for _, to := range all {
			err := Transition(from, to)
			if legal[from][to] {
				assert.NoErrorf(t, err, "%s -> %s 应为合法迁移", from, to)
			} else {
				var ite *InvalidTransitionError
				if assert.ErrorAsf(t, err, &ite, "%s -> %s 应为非法迁移", from, to) {
					assert.Equal(t, from, ite.From)
					assert.Equal(t, to, ite.To)
				}
			}
		}
	}
}

// TestChangeTransitionUnknownStatus 未知/空状态无出边，一律非法。
func TestChangeTransitionUnknownStatus(t *testing.T) {
	assert.Error(t, Transition(ChangeStatus(""), ChangeStatusDraft))
	assert.Error(t, Transition(ChangeStatus("bogus"), ChangeStatusCancelled))
}

// TestInvalidTransitionErrorMessage 错误消息携带 from/to（静态安全参数）。
func TestInvalidTransitionErrorMessage(t *testing.T) {
	err := Transition(ChangeStatusDraft, ChangeStatusCompleted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "draft")
	assert.Contains(t, err.Error(), "completed")
}

// TestIsTerminalChangeStatus 终态判定：5 终态为真，4 非终态为假。
func TestIsTerminalChangeStatus(t *testing.T) {
	for _, s := range TerminalChangeStatuses {
		assert.True(t, IsTerminalChangeStatus(s), "%s 应为终态", s)
	}
	for _, s := range ActiveChangeStatuses {
		assert.False(t, IsTerminalChangeStatus(s), "%s 不应为终态", s)
	}
	assert.False(t, IsTerminalChangeStatus(ChangeStatus("bogus")))
}

func pausedBatch(at time.Time) *BatchInfo {
	return &BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true, PausedAt: &at}
}

// TestCancelModeOf 取消路径分类（AC-3）：
// draft/pending_confirm → 整单；executing+批间暂停 → 整单；
// executing（未暂停）→ 中止；verifying 与全部终态 → 不可取消。
func TestCancelModeOf(t *testing.T) {
	cases := []struct {
		name   string
		status ChangeStatus
		batch  *BatchInfo
		want   CancelMode
	}{
		{"draft 整单取消", ChangeStatusDraft, nil, CancelModeWhole},
		{"pending_confirm 整单取消", ChangeStatusPendingConfirm, nil, CancelModeWhole},
		{"executing 无分批信息=中止", ChangeStatusExecuting, nil, CancelModeAbort},
		{"executing 未暂停=中止", ChangeStatusExecuting, &BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 5}, CancelModeAbort},
		{"executing 批间暂停=整单", ChangeStatusExecuting, pausedBatch(time.Now().Add(-time.Hour)), CancelModeWhole},
		{"executing 暂停标记但缺 pausedAt 仍整单", ChangeStatusExecuting, &BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true}, CancelModeWhole},
		{"verifying 不可取消", ChangeStatusVerifying, nil, CancelModeNone},
		{"verifying 即便带暂停标记也不可取消", ChangeStatusVerifying, pausedBatch(time.Now()), CancelModeNone},
		{"completed 不可取消", ChangeStatusCompleted, nil, CancelModeNone},
		{"partial_completed 不可取消", ChangeStatusPartialCompleted, nil, CancelModeNone},
		{"rolled_back 不可取消", ChangeStatusRolledBack, nil, CancelModeNone},
		{"rollback_failed 不可取消", ChangeStatusRollbackFailed, nil, CancelModeNone},
		{"cancelled 不可取消", ChangeStatusCancelled, nil, CancelModeNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, CancelModeOf(c.status, c.batch))
		})
	}
}
