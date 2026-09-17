// @feature cert-volcano-import-sync @api-functional
//
// Journey cloud-failure-isolation ( Medium ): per-cloud per-account failure
// isolation — a single cloud/account API failure records a static errorReason,
// the round settles partial_failed, all other units complete normally, and a
// rerun converges idempotently.
//
// Contract tests: one function per Contract outcome
// ( docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/ ).
// Journey smoke: TestCloudFailureIsolation_FullJourneySmoke.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package cloud_failure_isolation
