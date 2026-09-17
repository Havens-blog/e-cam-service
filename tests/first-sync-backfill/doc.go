// @feature cert-volcano-import-sync @api-functional
//
// Journey first-sync-backfill ( golden path, High ): the first ( empty-ledger
// ) daily sync backfills every cert-library instance of the reachable clouds
// into the ledger and establishes cloud certificate mappings — scheduler
// trigger, per-cloud per-account enumeration, fingerprint delta judgment,
// chain fetch + ledger entry, mapping establishment, session convergence.
//
// Contract tests: one function per Contract outcome
// ( docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/ ).
// Journey smoke: TestFirstSyncBackfill_FullJourneySmoke.
//
// ASSERTION_DEPTH: full behavioral coverage ( no exemption ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package first_sync_backfill
