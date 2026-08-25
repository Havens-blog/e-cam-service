// @feature cert-cloud-discovery-import @api-functional
//
// Package permission_boundary contains API functional tests generated from
// the permission-boundary journey contracts ( all four discovery endpoints
// are OpsEngineer-only; every other role is rejected with 403 and zero
// state side effects ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
//
// ASSERTION_DEPTH_EXEMPT ( partial ): this authorization-boundary journey's
// contract outputs are themselves HTTP 401/403 transport decisions, so its
// status-code assertions carry the business semantics ( analogous to the
// health-check exemption in rules/assertion-depth.md ). Behavioral coverage
// is supplemented where the contract defines State/Side-effect dimensions
// ( zero sessions, unchanged ledger, no cloud calls, no data leaks ).
package permission_boundary
