---
status: "completed"
started: "2026-09-17 13:06"
completed: "2026-09-17 14:25"
time_spent: "~1h 19m"
---

# Task Record: T-test-gen-scripts Generate API Functional Test Scripts

## Summary
Generated 82 API functional tests (77 contract outcomes 1:1 + 5 journey smokes) across 5 journeys under tests/<journey>/ flat layout (single api surface), driven by a new shared hermetic harness package tests/synctest wiring the production CertSyncService + synchronous import pipeline + scheduler cert:cert-import job entry over in-memory fakes and stubbed cloud ports (per-cloud CertLibraryLister / DiscoveryCertAdapter with Get-call counter / ScanAccountSource). All 5 packages green run sequentially; compile gate green.

## Changes

### Files Created
- tests/synctest/harness.go
- tests/first-sync-backfill/doc.go
- tests/first-sync-backfill/helpers_test.go
- tests/first-sync-backfill/step1_scheduler_trigger_test.go
- tests/first-sync-backfill/step2_enumerate_clouds_test.go
- tests/first-sync-backfill/step3_fingerprint_compare_test.go
- tests/first-sync-backfill/step4_import_unlisted_test.go
- tests/first-sync-backfill/step5_establish_mapping_test.go
- tests/first-sync-backfill/step6_session_convergence_test.go
- tests/first-sync-backfill/first_sync_backfill_smoke_test.go
- tests/incremental-skip-drift/doc.go
- tests/incremental-skip-drift/helpers_test.go
- tests/incremental-skip-drift/step1_detect_delta_set_test.go
- tests/incremental-skip-drift/step2_mapped_skip_test.go
- tests/incremental-skip-drift/step3_backfill_mapping_test.go
- tests/incremental-skip-drift/step4_drift_refresh_test.go
- tests/incremental-skip-drift/step5_old_mapping_reverse_lookup_test.go
- tests/incremental-skip-drift/step6_session_reconcile_test.go
- tests/incremental-skip-drift/incremental_skip_drift_smoke_test.go
- tests/manual-scheduler-race/doc.go
- tests/manual-scheduler-race/helpers_test.go
- tests/manual-scheduler-race/step1_scheduler_round_start_test.go
- tests/manual-scheduler-race/step2_manual_trigger_test.go
- tests/manual-scheduler-race/step3_overlapping_processing_test.go
- tests/manual-scheduler-race/step4_idempotent_absorption_test.go
- tests/manual-scheduler-race/step5_race_convergence_test.go
- tests/manual-scheduler-race/manual_scheduler_race_smoke_test.go
- tests/cloud-failure-isolation/doc.go
- tests/cloud-failure-isolation/helpers_test.go
- tests/cloud-failure-isolation/step1_trigger_round_test.go
- tests/cloud-failure-isolation/step2_enumerate_units_test.go
- tests/cloud-failure-isolation/step3_isolated_unit_failure_test.go
- tests/cloud-failure-isolation/step4_remaining_clouds_complete_test.go
- tests/cloud-failure-isolation/step5_terminal_and_rerun_test.go
- tests/cloud-failure-isolation/cloud_failure_isolation_smoke_test.go
- tests/manual-sync-endpoint-guard/doc.go
- tests/manual-sync-endpoint-guard/helpers_test.go
- tests/manual-sync-endpoint-guard/step1_manual_trigger_contract_test.go
- tests/manual-sync-endpoint-guard/step2_poll_via_sessionid_test.go
- tests/manual-sync-endpoint-guard/step3_failure_summary_contract_test.go
- tests/manual-sync-endpoint-guard/step4_repeat_trigger_test.go
- tests/manual-sync-endpoint-guard/manual_sync_endpoint_guard_smoke_test.go

### Files Modified
无

### Key Decisions
无

## Cases Generated
77

## Cases Evaluated
77

## Scripts Created
- tests/synctest/harness.go
- tests/first-sync-backfill/doc.go
- tests/first-sync-backfill/helpers_test.go
- tests/first-sync-backfill/step1_scheduler_trigger_test.go
- tests/first-sync-backfill/step2_enumerate_clouds_test.go
- tests/first-sync-backfill/step3_fingerprint_compare_test.go
- tests/first-sync-backfill/step4_import_unlisted_test.go
- tests/first-sync-backfill/step5_establish_mapping_test.go
- tests/first-sync-backfill/step6_session_convergence_test.go
- tests/first-sync-backfill/first_sync_backfill_smoke_test.go
- tests/incremental-skip-drift/doc.go
- tests/incremental-skip-drift/helpers_test.go
- tests/incremental-skip-drift/step1_detect_delta_set_test.go
- tests/incremental-skip-drift/step2_mapped_skip_test.go
- tests/incremental-skip-drift/step3_backfill_mapping_test.go
- tests/incremental-skip-drift/step4_drift_refresh_test.go
- tests/incremental-skip-drift/step5_old_mapping_reverse_lookup_test.go
- tests/incremental-skip-drift/step6_session_reconcile_test.go
- tests/incremental-skip-drift/incremental_skip_drift_smoke_test.go
- tests/manual-scheduler-race/doc.go
- tests/manual-scheduler-race/helpers_test.go
- tests/manual-scheduler-race/step1_scheduler_round_start_test.go
- tests/manual-scheduler-race/step2_manual_trigger_test.go
- tests/manual-scheduler-race/step3_overlapping_processing_test.go
- tests/manual-scheduler-race/step4_idempotent_absorption_test.go
- tests/manual-scheduler-race/step5_race_convergence_test.go
- tests/manual-scheduler-race/manual_scheduler_race_smoke_test.go
- tests/cloud-failure-isolation/doc.go
- tests/cloud-failure-isolation/helpers_test.go
- tests/cloud-failure-isolation/step1_trigger_round_test.go
- tests/cloud-failure-isolation/step2_enumerate_units_test.go
- tests/cloud-failure-isolation/step3_isolated_unit_failure_test.go
- tests/cloud-failure-isolation/step4_remaining_clouds_complete_test.go
- tests/cloud-failure-isolation/step5_terminal_and_rerun_test.go
- tests/cloud-failure-isolation/cloud_failure_isolation_smoke_test.go
- tests/manual-sync-endpoint-guard/doc.go
- tests/manual-sync-endpoint-guard/helpers_test.go
- tests/manual-sync-endpoint-guard/step1_manual_trigger_contract_test.go
- tests/manual-sync-endpoint-guard/step2_poll_via_sessionid_test.go
- tests/manual-sync-endpoint-guard/step3_failure_summary_contract_test.go
- tests/manual-sync-endpoint-guard/step4_repeat_trigger_test.go
- tests/manual-sync-endpoint-guard/manual_sync_endpoint_guard_smoke_test.go

## Test Results
82 test functions (77 outcomes 1:1 + 5 smokes), 5 packages all ok sequentially with -count=1: first-sync-backfill 19, incremental-skip-drift 19, manual-scheduler-race 16, cloud-failure-isolation 13, manual-sync-endpoint-guard 15; 0 failed, 1 documented t.Skip (timeout-partial-convergence). go build -p 1 ./... exit 0; go vet ./tests/... clean; gofmt clean (CRLF-normalized check); U+FFFD scan clean; no // VERIFY: markers; no duplicate test names.

## Acceptance Criteria
- [x] All 26 Contract files / 77 Outcomes have a 1:1 executable test function
- [x] Every journey has at least 1 smoke test (happy path + >=1 error path)
- [x] Compile gate green (go build -p 1 ./... + go vet ./tests/...)
- [x] All 5 generated journey packages pass sequentially with -count=1
- [x] Assertion depth: >=80% behavioral per journey, >=30% deep of behavioral
- [x] fixture_spec entities (CloudAccount/Certificate/CloudCertMapping/DiscoveryImportSession/CertLibraryInstance) carried by test fixtures
- [x] @feature tags + SKIP_EVAL_GATE headers on every generated file

## Notes
Quick mode (SKIP_EVAL_GATE=true, no .eval-report.md by design - matches gen-journeys/gen-contracts stages of this feature). Cross-validation ran in full degradation mode: contracts carry no endpoint/method anchors and no api handbook exists (quick-mode proposal feature) - Fact Table (90 facts incl. 30 CERT_SYNC_*) used as inference source; endpoint/method/error-code semantics manually cross-checked against CERT_SYNC_ENDPOINT / CERT_SYNC_CONFLICT_409 / CERT_SYNC_RUN_VO_FIELDS and verified consistent. Skip policy: exactly 1 documented skip (first-sync-backfill step-6 timeout-partial-convergence) - the 10-minute discoveryImportTimeout budget is owned by the service-internal context (cert_sync_service.go run() detaches from caller ctx, no external knob), not exhaustible at the API layer; timeout semantics covered by unit-layer TestCertSync_OverallTimeout (injected 50ms budget); follows the accepted first-ledger step5 precedent. Drift semantics note recorded in tests/incremental-skip-drift/doc.go: Drifted counter increments on the ledger-refresh shape (new fp already in ledger), while the common re-sign shape goes through the import pipeline (Imported=1) with new mapping row + old row retained - both shapes verified. Compile gate used go build -p 1 ./... per repo precedent (no justfile; Makefile mapping). Race fixtures use a gated lister stub with signal drain to prove CAS is held (SetGate drains stale Entered tokens). No real cloud accounts touched - all cloud boundaries stubbed per repo convention.
