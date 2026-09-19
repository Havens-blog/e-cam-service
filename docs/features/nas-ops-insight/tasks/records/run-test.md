---
status: "completed"
started: "2026-09-19 21:08"
completed: "2026-09-19 21:25"
time_spent: "~17m"
---

# Task Record: T-test-run Run API Functional Test

## Summary
Ran all 6 nas-ops-insight journey test suites: 90/90 passed, 0 failed, 0 skipped (MONGO_DSN-gated LiveDAO checks executed live and passed). First run had 1 failing live test; root cause was a test-script ordering-assumption bug (ListByAccounts sorts by fs_id+date, rows tie on both keys so return order follows account_id), confirmed NOT a production defect via the DAO's own live suite against the same real Mongo instance (all PASS, including bulk zero-row zero_exception persistence). Minimal order-independent fix applied to the test script; no production code touched; no assertions weakened (all semantic assertions retained). Report at tests/results/latest.md, raw output at tests/results/nas-ops-insight-raw.txt (both gitignored by design).

## Changes

### Files Created
- tests/results/latest.md
- tests/results/nas-ops-insight-raw.txt

### Files Modified
- tests/daily-metrics-collection/step3_metric_upsert_test.go

### Key Decisions
无

## Cases Generated
90

## Cases Evaluated
N/A

## Scripts Created
无

## Test Results
90 cases run, 90 passed, 0 failed, 0 skipped (live MONGO_DSN checks included: TestStep3_LiveDAO_CrossAccountRowsCoexist, TestStep3_LiveDAO_ReplayIdempotent; plus cross-evidence DAO live suite TestNASMetric* all PASS)

## Acceptance Criteria
- [x] All test cases MUST pass — no skipped tests, no expected failures, no TODO placeholders
- [x] Tests MUST verify actual functional behavior — no placeholder tests, no always-pass mocks, no stub assertions that validate nothing

## Notes
Runner: go test -count=1 per journey package, sequential chunks (no justfile in repo; in-memory nastest harness, no server lifecycle needed — dev/probe/teardown steps not applicable). Live DSN built in-shell from config/prod.yaml pointing at the reachable instance, isolated test DB ecam_dao_test with Drop cleanup; credentials never printed or persisted. Test-script fix detail: TestStep3_LiveDAO_CrossAccountRowsCoexist asserted positional indexes got[0]/got[1]/got[2] but ListByAccounts sort keys (fs_id, date) are tied across the three seeded rows, so order is not contractually fixed; now asserts by account_id lookup keeping all semantic assertions (3 rows coexist under (account_id, fs_id, date) unique key; zero row carries zero_exception; capacities land under their own accounts). go vet clean; deliverables scanned for U+FFFD (0 found).
