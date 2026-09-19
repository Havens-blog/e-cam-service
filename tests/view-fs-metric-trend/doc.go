// @feature nas-ops-insight @api-functional
//
// Journey view-fs-metric-trend ( Medium ): the operator requests a single
// filesystem's near-N-day trend through GET /assets/nas/metrics, reads the
// derived utilization and data_status annotations ( missing days annotated,
// zero_exception rows nulled, used>capacity clamped while raw used is
// returned ), and judges expansion timing from the day-value series peak ( the
// day-end snapshot architecture deliberately does not promise intra-day
// spikes ).
//
// Contract tests: one function per Contract outcome
// ( docs/features/nas-ops-insight/testing/view-fs-metric-trend/contracts/ ).
// Journey smoke: TestViewFsMetricTrend_FullJourneySmoke.
//
// Scope note: unauthorized-401 outcomes are exempt ( global auth middleware ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package view_fs_metric_trend
