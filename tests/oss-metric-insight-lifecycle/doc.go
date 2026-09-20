// @feature oss-ops-insight @api-functional
//
// Journey oss-metric-insight-lifecycle ( High, golden path ): the OSS insight
// end-to-end lifecycle — the persistent daily gate claims the oss key once per
// Asia/Shanghai day and submits oss:collect_metrics(days=2), the collect
// executor enumerates local OSS buckets per active account, metric rows land
// with first-write-wins for today / overwrite for yesterday under the
// (account_id, bucket_name, date) unique key, and the OSS console surfaces
// ( ops card / bucket trend / Top ) read exclusively from the ecam_oss_metric
// table — never from the asset snapshot.
//
// Contract tests: one function per Contract outcome
// ( docs/features/oss-ops-insight/testing/oss-metric-insight-lifecycle/contracts/ ).
// Journey smoke: TestOSSMetricInsightLifecycle_FullJourneySmoke.
//
// Scope notes:
//   - unauthorized-401 outcomes are exempt: authentication is enforced by the
//     global auth middleware outside the OSS surface ( see doc note in each
//     exempt test ).
//   - The scheduler trigger loop (checkOSSMetricsCollection) is unexported and
//     covered by internal unit tests; this suite drives the exported
//     PersistentDailyGate + taskx.Queue composition with the exact production
//     claim-then-submit semantics ( oss:collect_metrics, days=2 ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package oss_metric_insight_lifecycle
