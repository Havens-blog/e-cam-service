// @feature cert-volcano-import-sync @api-functional
//
// Journey manual-scheduler-race ( High ): dedup and idempotent convergence
// when the manual round and the scheduler round collide — the scheduler face
// yields silently on ErrSyncRunning, the manual face answers an immediate
// structured 409, and overlapping same-fingerprint writes are absorbed
// idempotently ( exactly one ledger row, both entries success ).
//
// Contract tests: one function per Contract outcome
// ( docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/ ).
// Journey smoke: TestManualSchedulerRace_FullJourneySmoke.
//
// Race modeling note: the CAS guard serializes rounds by design, so
// "overlapping rounds" at the test layer means (a) an in-flight round held by
// a gated lister while the other entry is rejected, and (b) sequential rounds
// re-processing the same fingerprint / intra-round duplicate absorption.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package manual_scheduler_race
