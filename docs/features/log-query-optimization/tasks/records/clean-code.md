---
status: "completed"
started: "2026-09-16 14:01"
completed: "2026-09-16 14:07"
time_spent: "~6m"
---

# Task Record: T-clean-code Simplify and Clean Code

## Summary
Feature-scoped code cleanup for log-query-optimization (both repos). Backend e-cam-service: deduplicated the Search/Aggregate SWR cache wrapper into a shared generic cachedCall helper (result_cache.go), removing the duplicated cache.get + shallow-copy + flag-write idiom; simplified keyPrefix to strings.IndexByte. Frontend e-cam-web: removed redundant 'network error' substring check in friendlySearchError ('network' already covers it). No behavior change; go build/vet + 13 targeted service tests pass, vue-tsc clean + 44 logs-view vitest tests pass.

## Changes

### Files Created
无

### Files Modified
- internal/logquery/service/result_cache.go
- internal/logquery/service/federation.go
- e-cam-web:src/views/logs/index.vue

### Key Decisions
- Scope limited to feature diff: backend commit e42c06e files + e-cam-web logs view files (ebe1962..HEAD); no out-of-scope files touched
- Extracted generic cachedCall[T] in result_cache.go rather than changing resultCache itself — preserves shallow-copy-on-read semantics (avoids data race on per-request Cached/CacheStale flags)
- Declined cosmetic churn: aliyun/aws 'hit = h' double assignment, LogStats mount polling, huawei/aliyun cache helpers are intentionally per-package (unexported, 5 lines each) — churn outweighs value

## Test Results
- **Tests Executed**: Yes
- **Passed**: 57
- **Failed**: 0
- **Coverage**: 0.0%

## Acceptance Criteria
- [x] Code simplified without changing external behavior
- [x] No files cleaned outside this feature's scope (git diff boundaries)

## Notes
testsPassed = 13 backend (go test ./internal/logquery/service/ targeted cache/search/aggregate run) + 44 frontend (vitest src/views/logs, 3 files). vue-tsc --noEmit clean. e-cam-web cleanup committed in its own repo; backend cleanup committed in e-cam-service.
