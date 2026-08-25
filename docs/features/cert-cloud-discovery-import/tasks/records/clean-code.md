---
status: "completed"
started: "2026-08-25 19:32"
completed: "2026-08-25 19:49"
time_spent: "~17m"
---

# Task Record: T-clean-code Simplify and Clean Code

## Summary
Scoped code-quality pass over cert-cloud-discovery-import via forge:clean-code skill. Scope resolved from feature context: backend commits 15bf620..d5198ce on main (44 Go files) + e-cam-web branch feat/cert-cloud-discovery-import commits 1047f27/43d14c6/141def7 (8 code/config files; 559435a base commit excluded as it belongs to ssl-cert-management). 5 files modified, 47 already clean: 7 U+FFFD corrupted-character comment repairs (3 backend Go files, 1 frontend .vue with 3 spots) reconstructed from authoritative parallel text / paren-balance, plus 1 dead-code removal (leftover '_ = ctx' in huawei GetCert, replaced by '_ context.Context' param per certtest convention). No external behavior changed. Quality gates green: backend go test 23 packages ok (564 PASS incl subtests, 33 SKIP DSN-gated integration by design, 0 FAIL), frontend vitest 15 files / 357 tests passed, gofmt real-delta 0 on all edited files (repo-wide CRLF noise pre-existing), go vet clean.

## Changes

### Files Created
无

### Files Modified
- internal/cert/domain/errors.go
- internal/cert/domain/repository.go
- internal/cert/repository/repos_smoke_test.go
- internal/shared/cloudx/huawei/cert_discovery.go
- e-cam-web:src/views/cert/ledger/components/DiscoveryImportModal.vue

### Key Decisions
- Scope via feature commits, not git diff: backend feature commits are already merged to main (git diff main...HEAD empty), so Priority-3 feature-context resolution was used; frontend branch diff was narrowed to the 3 discovery commits to exclude the 559435a cert-module base belonging to ssl-cert-management
- repository.go U+FFFD corruption ('already claimed' comment) was born inside feature commit ae53269 - no clean original exists anywhere in git history; reconstructed as 'claimed by another worker' from CAS claim semantics parallel to execute_service.go wording
- DiscoveryImportModal.vue 3 U+FFFD spots reconstructed by parenthesis balance: two unclosed '(' comment openers took ')', and the ')' at end of the following line paired with an opener at the third spot
- huawei GetCert unused-ctx cleanup limited to GetCert only; UploadCert/BindResource/CleanupOrphan keep their grouped '_ = ctx/_ = creds/...' blanking idiom (established file convention for multi-param stubs)
- Did NOT deduplicate the 4 near-identical cloud adapter shim closures in discovery_import_service.go: Go generics cannot field-access type params, per-cloud doc comments carry assembly context, and an extractor-closure indirection would reduce clarity (skill principle 4)
- U+FFFD scan performed across full backend + frontend scope, not just edited files (repo hazard precedent); fixes applied via ASCII-source node scripts with post-write self-checks

## Test Results
- **Tests Executed**: Yes
- **Passed**: 921
- **Failed**: 0
- **Coverage**: 26.6%

## Acceptance Criteria
- [x] Code simplified without changing external behavior
- [x] No files cleaned outside this feature's scope (git diff boundaries)

## Notes
Gate recipe mapping (repo has no justfile, per 1.gate/2.gate precedent): unit-test = go test per-package sequential then full tree with -p 2 (OOM hazard), frontend = vitest run. -race unavailable on this host (no cgo/gcc). Coverage 26.6% is the merged per-package plain -cover statement total across the cert+cloudx tree (go tool cover -func on concatenated profiles); volcano package excluded from coverage instrumentation only - pre-existing BOM in volcano/kafka.go breaks build under -cover instrumentation but passes normally (green in full test run, 18 PASS counted separately). 33 backend SKIPs are CERT_TEST_MONGODB_DSN-gated repository integration tests, skip by design. One mid-gate regression: my own incomplete huawei edit (script hit the wrong one of three '_ = ctx' lines, leaving an undefined ctx) broke the build during the first gate pass; fixed within the gate loop (restore UploadCert blank line, remove GetCert leftover, strip stray blank line) and the re-run is fully green - counted as fixed, not reverted. All 7 U+FFFD repairs are comment-only; backend rescan and frontend rescan both show zero remaining U+FFFD in scope. Frontend change sits uncommitted on e-cam-web branch feat/cert-cloud-discovery-import to be committed alongside this record (same pattern as prior feature tasks).
