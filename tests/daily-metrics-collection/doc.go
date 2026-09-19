// @feature nas-ops-insight @api-functional
//
// Journey daily-metrics-collection ( High ): the persistent daily gate claims
// each Asia/Shanghai day exactly once, the collect executor enumerates local
// NAS instances per active account, metric rows land with first-write-wins
// for today / overwrite for yesterday under the (account_id, fs_id, date)
// unique key, and service restarts never duplicate the daily submission.
//
// Contract tests: one function per Contract outcome
// ( docs/features/nas-ops-insight/testing/daily-metrics-collection/contracts/ ).
// Journey smoke: TestDailyMetricsCollection_FullJourneySmoke.
//
// Scope note: the scheduler trigger loop (checkNASMetricsCollection) is
// unexported and covered by internal unit tests; this suite drives the
// exported PersistentDailyGate + taskx.Queue composition with the exact
// production claim-then-submit semantics ( nas:collect_metrics, days=2 ).
// The memory-gate rollback branch ( rollback-memory-gate-active ) is likewise
// internal-only and exercised by internal scheduler tests — documented as an
// exemption rather than silently dropped.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package daily_metrics_collection
