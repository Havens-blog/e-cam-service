// @feature cert-cloud-discovery-import @api-functional
//
// Package unsupported_entries_skip contains API functional tests generated
// from the unsupported-entries-skip journey contracts ( huawei and AWS
// IAM-hosted entries form the unselectable group; forced submissions are
// skipped per item with static reasons and never block the session ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
//
// ASSERTION_DEPTH_EXEMPT ( partial ): the unauthorized outcome asserts the
// 401 transport gate ( structural by the assertion-depth rubric ); skip
// reasons, write-free state and session convergence are asserted
// behaviorally and deep.
package unsupported_entries_skip
