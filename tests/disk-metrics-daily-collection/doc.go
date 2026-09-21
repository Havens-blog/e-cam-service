// @feature disk-ops-insight @api-functional
//
// Journey disk-metrics-daily-collection ( High, golden path ): the daily disk
// metric collection main line — the persistent daily gate disk key claim, the
// active-account enumeration over ecam_instance disk assets, per-vendor
// region-scoped DiskMetricQuerier calls, the (account_id, disk_id, date)
// unique-keyed persistence with first-write-wins for today rows and overwrite
// for past rows, the observable Result summary, and the read-your-own-data
// disk trend view.
//
// Contract tests: one function per Contract outcome
// ( docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/contracts/ ).
// Journey smoke: TestDiskMetricsDailyCollection_FullJourneySmoke.
//
// Scope notes ( asserted exemptions / anchored code-reality ):
//   - unauthorized-401 ( step-6 ) is exempt ( global auth middleware, see
//     per-test note ).
//   - account-busy-skip ( step-2 ) is exempt: the account mutex lives in the
//     unexported executor gate ( nasAccountGate.tryAcquireAccount ) and is
//     covered by the internal unit suite
//     internal/cam/task/executor/account_lock_test.go.
//   - The disk surface itself has no capacity field: "字节 → GB 走共享
//     types.BytesToGB" is not applicable ( AC 「如适用」 ), asserted via the
//     metric-table-only data source ( snapshot sentinel never leaks ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package disk_metrics_daily_collection
