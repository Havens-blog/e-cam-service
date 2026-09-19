---
status: "completed"
started: "2026-09-19 14:52"
completed: "2026-09-19 15:06"
time_spent: "~14m"
---

# Task Record: T-test-gen-scripts Generate API Functional Test Scripts

## Summary
Generated API functional test scripts for cert-volcano-deployer under the dedup constraint: 4 of 5 journeys already implemented by task-5 functional tests in tests/volcano-cert-replacement (referenced, not regenerated); minimal supplement of 3 new test functions covering the NEW contract surfaces — ProductAwareUploader channel fallback (step-4 fallback-transparent, ErrVolcanoCSVCertNotBindable surfaced as documented csv bind gap), product-directed {product}:{id} ID-space mutex across cdn/waf/alb/nlb (steps 1-3), and GetCert mapping-fallback fail-safe boundaries (rollback step-2 mapping-fallback-no-record / invalid-target-rejected). Package 7/7 test functions green; gofmt/vet/build -p 1 clean.

## Changes

### Files Created
- tests/volcano-cert-replacement/volcano_product_aware_upload_test.go

### Files Modified
无

### Key Decisions
无

## Cases Generated
62

## Cases Evaluated
N/A

## Scripts Created
- tests/volcano-cert-replacement/volcano_product_aware_upload_test.go

## Test Results
3 new test functions (fallback-without-port / id-space-mutex / getcert-fallback-no-record) all PASS; full package go test -count=1 green (7 top-level functions incl. the 4 referenced task-5 journeys); go vet clean; go build -p 1 ./... exit 0; zero U+FFFD, zero VERIFY markers, gofmt clean

## Acceptance Criteria
- [x] All 20 contracts / 62 outcomes mapped to script coverage
- [x] Covered outcomes referenced to existing tests instead of regenerated (dedup constraint)
- [x] New testable surfaces supplemented: ProductAwareUploader fallback + directed upload, {product}:{id} normalization, ErrVolcanoCSVCertNotBindable, GetCert mapping fallback
- [x] Go test form aligned with tests/volcano-cert-replacement precedent (multicloudtest harness + StubVolcanoCertLibrary fake SDK stub)
- [x] Compile gate passed (gofmt / go vet / go build -p 1 / package tests green)

## Notes
SKIP_EVAL_GATE mode (quick pipeline): supplement file carries the SKIP_EVAL_GATE header. Layout deviation documented: single api surface normally maps journeys to tests/<journey>/ dirs, but per the dispatcher dedup constraint the supplement lives inside the existing tests/volcano-cert-replacement package to reuse the 539-line 21-method fake SDK stub (a fresh package would duplicate it). Coverage-by-reference anchors: two-phase/dispatch -> TestVolcanoCertReplacement_FullLifecycleSmoke + TestVolcanoChannelDispatch_ProductVisibility; bind-failure-compensation -> TestVolcanoCertReplacement_BindFailureCompensation + unit bind sentinels (internal/cert/deployer/volcano_deployer_test.go: ErrVolcanoCSVCertNotBindable / product mismatch / empty resource id / non-normalized cleanup id / ErrVolcanoProductNotSupported / bounded rate-limit retry with shared uploadCertIntoLibrary closure => fresh upload name per attempt); two-phase step-2 batch gate + step-5 probe-mismatch hold -> tests/multicloud-cert-replacement engine tests (cloud-agnostic); rollback step-3/4 -> TestVolcanoCertReplacement_RollbackRestoresOldCertPerProduct. Outcomes referenced to generic engine/cloud-agnostic layers rather than duplicated: chain-missing-root-fallback, key-missing-intercepted, upload-name-conflict-idempotent, retry-fresh-upload-name (engine-level TestExecuteUploadPhase_RateLimitedRetriesWithNewName + unit backoff), delete-api-failure-queued / concurrent-retry-race / mapping-reverse-lookup-failure (channel compensation semantics), five-cloud-regression-intact (three-cloud-product-matrix), scan-api-failure-isolated / unknown-product-placeholder-fingerprint (scan adapter layer). Partial-port-implementation outcome implemented as instance-level type-assertion fallback (per-product intra-port fallback does not exist in code; csv -> ErrVolcanoProductNotSupported fail-fast is the port-level sentinel) — asserted via the runtime interface-satisfaction check in the fallback test.
