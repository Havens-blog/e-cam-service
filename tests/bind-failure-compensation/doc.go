// @feature cert-multicloud-deployers @api-functional
//
// Package bind_failure_compensation hosts the API-functional tests for the
// "bind-failure-compensation" journey: the post-bind-failure compensation
// state machine ( explicit failure -> CleanupOrphan -> mapping active->orphan
// -> cleanup queue consumption ) shared by all three new clouds.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package bind_failure_compensation
