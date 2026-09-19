// @feature nas-ops-insight @api-functional
//
// Journey vendor-failure-observability ( High ): best-effort vendor failures
// never block the collect flow — "probe unsupported" stays INFO-level and out
// of the failure counters while real API failures land in Result.failures
// with error_count/last_error, zero-capacity rows stay visible as
// zero_exception, mandatory providers with zero success rows and >=1 live NAS
// instance escalate a critical health alert, and the empty-state distinction
// ( truly empty / collect failed / zero_exception ) is observable on the api
// surface via Result.failures plus data_status.
//
// Contract tests: one function per Contract outcome
// ( docs/features/nas-ops-insight/testing/vendor-failure-observability/contracts/ ).
// Journey smoke: TestVendorFailureObservability_FullJourneySmoke.
//
// Scope note: the display derivation itself ( step 3 branches ) is frontend
// logic; the api surface contract asserted here is the distinguishability of
// the three states from Result.failures and data_status.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package vendor_failure_observability
