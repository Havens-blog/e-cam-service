package domain

import "errors"

var (
	ErrBillingAuthFailed      = errors.New("billing authentication failed")
	ErrBillingAPITimeout      = errors.New("billing API request timeout")
	ErrBillingRateLimited     = errors.New("billing API rate limited")
	ErrCollectLockFailed      = errors.New("failed to acquire collect lock")
	ErrCollectAlreadyRunning  = errors.New("collect task already running for this account")
	ErrBudgetScopeInvalid     = errors.New("budget scope target not found")
	ErrAllocationRuleConflict = errors.New("allocation rule conflict")
	ErrAllocationRatioInvalid = errors.New("allocation ratio sum must equal 100%")
	ErrAllocationDimExceed    = errors.New("allocation rule exceeds max 5 dimension combos")
	ErrAllocationDimInvalid   = errors.New("invalid allocation dimension type")
	ErrNormalizeMissingField  = errors.New("required field missing in raw bill")
)
