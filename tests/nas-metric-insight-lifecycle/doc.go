// @feature nas-ops-insight @api-functional
//
// Journey nas-metric-insight-lifecycle ( High, golden path ): the full loop
// from the persistent daily gate claiming the collect task, vendor metrics
// landing in GB under the (account_id, fs_id, date) unique key, operators
// reading per-fs trends through GET /assets/nas/metrics, the ops-card
// aggregation ( fs-level dedup + warning priority ), through GET /assets/nas/top
// ranking that surfaces high-watermark instances.
//
// Contract tests: one function per Contract outcome
// ( docs/features/nas-ops-insight/testing/nas-metric-insight-lifecycle/contracts/ ).
// Journey smoke: TestNASMetricInsightLifecycle_FullJourneySmoke.
//
// Scope notes:
//   - unauthorized-401 outcomes are exempt: authentication is enforced by the
//     global auth middleware, not by the NAS read surface itself; the handler
//     and service layers carry no auth logic of their own.
//   - collect-failure-warning-priority / zero-or-no-data-placeholder are
//     display branches derived on the frontend from the task Result; the api
//     surface contract asserted here is that Result.failures makes "collect
//     failed" and "truly empty" distinguishable ( see step 4 tests ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package nas_metric_insight_lifecycle
