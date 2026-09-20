---
status: "completed"
started: "2026-09-20 12:35"
completed: "2026-09-20 12:47"
time_spent: "~12m"
---

# Task Record: 5 oss:collect_metrics 采集执行器(账号遍历 + 首写生效 + 注册)

## Summary
Implemented oss:collect_metrics executor (SyncOSSMetricsExecutor): active-account iteration (ecam_instance oss buckets, no EnableAutoSync filter), OSSMetricQuerier per-bucket collection, today rows first-write-wins via DAO BulkInsertIfAbsent / yesterday rows overwrite via BulkUpsertMetrics, account mutex reused from nasAccountGate, bounded bucket concurrency 5, failure accounting into Result[failures] (provider/account_id/error_count/last_error; probe-unsupported and genuine no-data not counted), 31-day backfill window clamp, registered in task module.go. 12 new unit tests all green; go build ./... passes.

## Changes

### Files Created
- internal/cam/task/executor/sync_oss_metrics.go
- internal/cam/task/executor/sync_oss_metrics_test.go

### Files Modified
- internal/cam/task/module.go

### Key Decisions
- Reused shared package constructs instead of re-creating: nasAccountGate (embedded, Hard Rule), resolveNASAccounts, nasAccountFailure/nasProviderFailure, nasMetricsDateRange/nasMetricsCSTZone; OSS constants alias NAS ones as single source
- AccountID/Provider backfilled by executor (querier signature is account-agnostic); QcStatus passed through untouched, zero_exception tagging stays in DAO ossMetricQC write path
- collectBucketMetrics returns -1 for failed bucket so failedBuckets can be counted under bounded concurrency without breaking other buckets
- Dedicated test providers (ossmetric-yes/no/fail/empty/nooss) registered in init to avoid global registry mutation between tests

## Test Results
- **Tests Executed**: Yes
- **Passed**: 12
- **Failed**: 0
- **Coverage**: 67.7%

## Acceptance Criteria
- [x] Executor iterates active accounts (ecam_instance oss buckets, no EnableAutoSync dependency) and writes ecam_oss_metric via OSSMetricQuerier
- [x] Today rows first-write-wins (BulkInsertIfAbsent), yesterday rows overwrite; no CDN all-zero filter (capacity=0 stored with zero_exception by DAO)
- [x] Account-level mutex + bucket bounded concurrency 5 via reused nasAccountGate
- [x] Failure counts in Result[failures] (provider/account/error_count/last_error); probe-unsupported and genuine no-data not counted as failure
- [x] Registered in module.go; oss:collect_metrics task submittable by scheduler
- [x] Unit tests for account iteration / first-write / failure counting / mutex; go build ./... passes

## Notes
Coverage 67.7% is the executor package runner figure; new file per-function coverage ~79-100% (GetType 100, Execute 87.5, collectAccount 82.1, collectBucketMetrics 79.4). -race unavailable on this host (no cgo/gcc) per established convention; go vet clean on internal/cam/task/... just lint reports 3 pre-existing e-cam-web frontend errors (cost/statistics/task.ts empty interfaces) unrelated to this backend-only change.
