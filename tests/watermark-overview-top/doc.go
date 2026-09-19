// @feature nas-ops-insight @api-functional
//
// Journey watermark-overview-top ( Medium ): the ops-card watermark
// aggregation dedups by fs_id ( shared fs counted once via the
// "latest date then largest capacity" representative row ), the average
// utilization denominator skips instance-less and capacity=0 rows, GET
// /assets/nas/top validates its parameter bounds ( days 1~90, sort
// capacity|utilization, top/page_size max 50 ), answers empty pages with 200
// + correct pagination metadata, and the high-watermark instance identified
// from the ranking drills down into the trend drawer by fs_id.
//
// Contract tests: one function per Contract outcome
// ( docs/features/nas-ops-insight/testing/watermark-overview-top/contracts/ ).
// Journey smoke: TestWatermarkOverviewTop_FullJourneySmoke.
//
// Scope notes: unauthorized-401 outcomes are exempt ( global auth middleware );
// the collect-failure-warning-priority display branch is frontend-derived and
// asserted via the api signal ( Result.failures non-empty ) — see the
// vendor-failure-observability suite for the full distinguishability matrix.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package watermark_overview_top
