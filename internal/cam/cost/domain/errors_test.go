package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSentinelErrors 固化哨兵错误的当前消息文案与身份：
// 上层（handler/服务层）通过 errors.Is 与文案透出消费这些错误，
// 文案变更或误改指针都会破坏调用方匹配，此处固化当前值。
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ErrBillingAuthFailed", ErrBillingAuthFailed, "billing authentication failed"},
		{"ErrBillingAPITimeout", ErrBillingAPITimeout, "billing API request timeout"},
		{"ErrBillingRateLimited", ErrBillingRateLimited, "billing API rate limited"},
		{"ErrCollectLockFailed", ErrCollectLockFailed, "failed to acquire collect lock"},
		{"ErrCollectAlreadyRunning", ErrCollectAlreadyRunning, "collect task already running for this account"},
		{"ErrBudgetScopeInvalid", ErrBudgetScopeInvalid, "budget scope target not found"},
		{"ErrAllocationRuleConflict", ErrAllocationRuleConflict, "allocation rule conflict"},
		{"ErrAllocationRatioInvalid", ErrAllocationRatioInvalid, "allocation ratio sum must equal 100%"},
		{"ErrAllocationDimExceed", ErrAllocationDimExceed, "allocation rule exceeds max 5 dimension combos"},
		{"ErrAllocationDimInvalid", ErrAllocationDimInvalid, "invalid allocation dimension type"},
		{"ErrNormalizeMissingField", ErrNormalizeMissingField, "required field missing in raw bill"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, tc.err)
			assert.EqualError(t, tc.err, tc.want)
			assert.ErrorIs(t, tc.err, tc.err)
		})
	}
}

// TestSentinelErrorsDistinct 各哨兵错误互不相同（避免复制粘贴时误共用同一实例）。
func TestSentinelErrorsDistinct(t *testing.T) {
	all := []error{
		ErrBillingAuthFailed, ErrBillingAPITimeout, ErrBillingRateLimited,
		ErrCollectLockFailed, ErrCollectAlreadyRunning, ErrBudgetScopeInvalid,
		ErrAllocationRuleConflict, ErrAllocationRatioInvalid, ErrAllocationDimExceed,
		ErrAllocationDimInvalid, ErrNormalizeMissingField,
	}
	seen := make(map[string]bool, len(all))
	for _, err := range all {
		msg := err.Error()
		assert.False(t, seen[msg], "duplicate sentinel message: %s", msg)
		seen[msg] = true
	}
	assert.Len(t, seen, len(all))
}
