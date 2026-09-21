// @feature disk-ops-insight @api-functional
//
// Journey disk-metrics-query ( Low, read-only ): the disk metric read
// surface — single-disk trend ( GET /assets/disk/metrics ) and the
// account-scoped Top ( GET /assets/disk/top ), parameter bounds
// ( days ∈ [1,90], sort ∈ {usage_percent,iops,throughput}, top ≤ 50 ),
// tenant isolation ( cross-tenant account answers 404 without leaking
// existence ), the qc_status → data_status closure for zero-usage anomaly
// rows, and honest empty windows ( null values, never fake zeros ).
//
// Contract tests: one function per Contract outcome
// ( docs/features/disk-ops-insight/testing/disk-metrics-query/contracts/ ).
// Journey smoke: TestDiskMetricsQuery_FullJourneySmoke.
//
// Scope notes: unauthorized-401 outcomes are exempt ( global auth middleware,
// see per-test notes ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package disk_metrics_query
