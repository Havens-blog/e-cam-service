// @feature cert-cloud-discovery-import @api-functional
//
// Package no_snapshot_guidance contains API functional tests generated from
// the no-snapshot-guidance journey contracts ( zero-snapshot guidance loop:
// NO_SNAPSHOT -> trigger scan -> poll snapshot-status -> done -> preview ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
//
// ASSERTION_DEPTH_EXEMPT ( partial ): the guidance loop's unauthorized
// outcomes and each polling iteration legitimately assert transport-layer
// facts ( 401 gate, 200 polls ), which the 80% behavioral threshold counts
// as structural; the snapshot state transitions ( running/done/failed ),
// partial failure payloads and session handover are asserted behaviorally.
package no_snapshot_guidance
