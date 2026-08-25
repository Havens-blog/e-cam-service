// @feature cert-cloud-discovery-import @api-functional
//
// Package first_ledger_import contains API functional tests generated from
// the first-ledger-import journey contracts ( empty ledger first
// registration via preview-confirm two-step discovery import ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
//
// ASSERTION_DEPTH_EXEMPT ( partial ): the unauthorized/validation outcomes
// assert transport-layer facts ( 401 gate, 400/404 envelopes ) that count as
// structural under rules/assertion-depth.md; ledger/mapping state, dedup,
// convergence and replay semantics are asserted behaviorally and deep.
package first_ledger_import
