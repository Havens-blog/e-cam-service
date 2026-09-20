// @feature oss-ops-insight @api-functional
//
// Journey view-bucket-metric-trend ( Low ): the single-bucket dual-series
// trend — capacity(GB) and object-count series with latest/average summaries
// from the metric table, days windows bounded 1~90, missing days annotated
// with data_status=missing ( never fake-filled ), zero_exception rows exposed
// verbatim ( storage_size=0 with the qc mapping ), and capacity-0 rows never
// divide-by-zero. unauthorized-401 is exempt ( global auth middleware ).
//
// Contract tests: one function per Contract outcome
// ( docs/features/oss-ops-insight/testing/view-bucket-metric-trend/contracts/ ).
// Journey smoke: TestViewBucketMetricTrend_FullJourneySmoke.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package view_bucket_metric_trend
