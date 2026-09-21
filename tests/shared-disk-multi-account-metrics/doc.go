// @feature disk-ops-insight @api-functional
//
// Journey shared-disk-multi-account-metrics ( High ): the same disk_id managed
// by multiple cloud accounts — the (account_id, disk_id, date) unique key
// keeps one row per account ( never merged or overwritten across accounts ),
// an account's collect failure never touches the other account's rows, empty
// -date rows are skipped before they could not locate a unique key, trend
// reads stay precisely scoped to the requested account, Top dedups by disk_id
// into a single representative row ( date desc, then usage desc — never
// summed across accounts ) with dedup applied before paging and aggregation,
// removed accounts' rows drop out of the current tenant scope, and busy_share
// idle-disk zeros count as real data.
//
// Contract tests: one function per Contract outcome
// ( docs/features/disk-ops-insight/testing/shared-disk-multi-account-metrics/contracts/ ).
// Journey smoke: TestSharedDiskMultiAccountMetrics_FullJourneySmoke.
//
// Scope notes: unauthorized-401 outcomes are exempt ( global auth middleware,
// see per-test notes ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package shared_disk_multi_account_metrics
