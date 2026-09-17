// @feature cert-volcano-import-sync @api-functional
//
// Journey incremental-skip-drift ( High ): incremental sync correctness over
// an existing ledger — mapped fingerprints skip at zero material cost,
// missing mappings backfill, re-issued cloud certs ( same cloudCertID, new
// fingerprint ) refresh via new mapping rows while old rows are retained,
// and the reverse lookup always resolves the latest fingerprint.
//
// Contract tests: one function per Contract outcome
// ( docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/ ).
// Journey smoke: TestIncrementalSkipDrift_FullJourneySmoke.
//
// Drift vocabulary note ( mirrors the service judgment ): when the new
// fingerprint is already in the ledger the round counts Drifted and refreshes
// the mapping directly; when the new fingerprint is unseen the instance goes
// through the import pipeline ( Imported==1 ) which writes the new mapping —
// both shapes retain the old mapping row and flip the reverse lookup to the
// new fingerprint.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package incremental_skip_drift
