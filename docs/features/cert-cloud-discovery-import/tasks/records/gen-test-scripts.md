---
status: "completed"
started: "2026-08-25 20:19"
completed: "2026-08-25 21:16"
time_spent: "~57m"
---

# Task Record: T-test-gen-scripts Generate API Functional Test Scripts

## Summary
Generated API functional test scripts for all 6 cert-cloud-discovery-import journeys from the 27 contract files (commit 433a341, 88 outcomes). Single-surface project (config surfaces: api), so output follows tests/<journey>/ with no surface-key layer. Produced 45 Go files: a shared hermetic harness package (tests/discoverytest) that wires the production HTTP surface (gin engine: Recovery -> auth-gate stub -> real CertRoleMiddleware -> real RequireRoles mounted by DiscoveryHandler/ReferenceHandler) over certtest in-memory repositories and stubbed cloud ports (per-cloud material adapter with call accounting, account source, scan trigger with async/in-progress modes, counting session repo), plus 33 test scripts per journey directory (27 step-contract files + 6 smoke files) and package doc/fixture files. Every contract outcome maps 1:1 to one test function (88 outcome tests + 6 smoke = 94 test functions). Validation: gofmt clean, go vet ./tests/... clean, go build -p 1 ./... OK, go test -count=1 ./tests/... all 6 packages pass (1 documented t.Skip for the session-timeout outcome: the 10-minute batchProcessTimeout budget cannot elapse in API-functional scope). Every generated file carries the @feature cert-cloud-discovery-import @api-functional tag and the SKIP_EVAL_GATE header per Quick-mode pipeline rules.

## Changes

### Files Created
- tests/discoverytest/harness.go
- tests/first-ledger-import/doc.go
- tests/first-ledger-import/helpers_test.go
- tests/first-ledger-import/step1_preview_request_test.go
- tests/first-ledger-import/step2_preview_fields_test.go
- tests/first-ledger-import/step3_confirm_import_test.go
- tests/first-ledger-import/step4_session_item_processing_test.go
- tests/first-ledger-import/step5_poll_progress_test.go
- tests/first-ledger-import/step6_rerun_failures_test.go
- tests/first-ledger-import/first_ledger_import_smoke_test.go
- tests/permission-boundary/doc.go
- tests/permission-boundary/permission_boundary_test.go
- tests/permission-boundary/step1_read_endpoints_403_test.go
- tests/permission-boundary/step2_write_endpoints_403_test.go
- tests/permission-boundary/step3_opsengineer_contrast_test.go
- tests/permission-boundary/permission_boundary_smoke_test.go
- tests/no-snapshot-guidance/doc.go
- tests/no-snapshot-guidance/step1_preview_no_snapshot_guidance_test.go
- tests/no-snapshot-guidance/step2_trigger_scan_test.go
- tests/no-snapshot-guidance/step3_poll_snapshot_status_test.go
- tests/no-snapshot-guidance/step4_done_enter_preview_test.go
- tests/no-snapshot-guidance/no_snapshot_guidance_smoke_test.go
- tests/unsupported-entries-skip/doc.go
- tests/unsupported-entries-skip/unsupported_entries_skip_test.go
- tests/unsupported-entries-skip/step1_unselectable_group_test.go
- tests/unsupported-entries-skip/step2_confirm_mixed_import_test.go
- tests/unsupported-entries-skip/step3_item_skip_reasons_test.go
- tests/unsupported-entries-skip/step4_terminal_reasons_test.go
- tests/unsupported-entries-skip/unsupported_entries_skip_smoke_test.go
- tests/placeholder-fingerprint-backfill/doc.go
- tests/placeholder-fingerprint-backfill/placeholder_fingerprint_backfill_test.go
- tests/placeholder-fingerprint-backfill/step1_placeholder_marked_test.go
- tests/placeholder-fingerprint-backfill/step2_confirm_import_test.go
- tests/placeholder-fingerprint-backfill/step3_parse_real_fingerprint_test.go
- tests/placeholder-fingerprint-backfill/step4_backfill_references_test.go
- tests/placeholder-fingerprint-backfill/step5_verify_reference_list_test.go
- tests/placeholder-fingerprint-backfill/placeholder_fingerprint_backfill_smoke_test.go
- tests/duplicate-concurrent-mapping/doc.go
- tests/duplicate-concurrent-mapping/duplicate_concurrent_mapping_test.go
- tests/duplicate-concurrent-mapping/step1_dual_channel_inledger_test.go
- tests/duplicate-concurrent-mapping/step2_confirm_import_group_test.go
- tests/duplicate-concurrent-mapping/step3_first_entry_register_test.go
- tests/duplicate-concurrent-mapping/step4_second_entry_backfill_test.go
- tests/duplicate-concurrent-mapping/step5_verify_convergence_test.go
- tests/duplicate-concurrent-mapping/duplicate_concurrent_mapping_smoke_test.go

### Files Modified
无

### Key Decisions
无

## Cases Generated
94

## Cases Evaluated
N/A

## Scripts Created
- tests/first-ledger-import/step1_preview_request_test.go
- tests/first-ledger-import/step2_preview_fields_test.go
- tests/first-ledger-import/step3_confirm_import_test.go
- tests/first-ledger-import/step4_session_item_processing_test.go
- tests/first-ledger-import/step5_poll_progress_test.go
- tests/first-ledger-import/step6_rerun_failures_test.go
- tests/first-ledger-import/first_ledger_import_smoke_test.go
- tests/permission-boundary/step1_read_endpoints_403_test.go
- tests/permission-boundary/step2_write_endpoints_403_test.go
- tests/permission-boundary/step3_opsengineer_contrast_test.go
- tests/permission-boundary/permission_boundary_smoke_test.go
- tests/no-snapshot-guidance/step1_preview_no_snapshot_guidance_test.go
- tests/no-snapshot-guidance/step2_trigger_scan_test.go
- tests/no-snapshot-guidance/step3_poll_snapshot_status_test.go
- tests/no-snapshot-guidance/step4_done_enter_preview_test.go
- tests/no-snapshot-guidance/no_snapshot_guidance_smoke_test.go
- tests/unsupported-entries-skip/step1_unselectable_group_test.go
- tests/unsupported-entries-skip/step2_confirm_mixed_import_test.go
- tests/unsupported-entries-skip/step3_item_skip_reasons_test.go
- tests/unsupported-entries-skip/step4_terminal_reasons_test.go
- tests/unsupported-entries-skip/unsupported_entries_skip_smoke_test.go
- tests/placeholder-fingerprint-backfill/step1_placeholder_marked_test.go
- tests/placeholder-fingerprint-backfill/step2_confirm_import_test.go
- tests/placeholder-fingerprint-backfill/step3_parse_real_fingerprint_test.go
- tests/placeholder-fingerprint-backfill/step4_backfill_references_test.go
- tests/placeholder-fingerprint-backfill/step5_verify_reference_list_test.go
- tests/placeholder-fingerprint-backfill/placeholder_fingerprint_backfill_smoke_test.go
- tests/duplicate-concurrent-mapping/step1_dual_channel_inledger_test.go
- tests/duplicate-concurrent-mapping/step2_confirm_import_group_test.go
- tests/duplicate-concurrent-mapping/step3_first_entry_register_test.go
- tests/duplicate-concurrent-mapping/step4_second_entry_backfill_test.go
- tests/duplicate-concurrent-mapping/step5_verify_convergence_test.go
- tests/duplicate-concurrent-mapping/duplicate_concurrent_mapping_smoke_test.go

## Test Results
94 test functions generated (88 contract outcomes 1:1 + 6 journey smoke). All 6 journey packages pass fresh: go test -count=1 ./tests/<journey>/ -> 6x ok, 0 failed, 1 documented t.Skip (first-ledger-import step5 session-timeout-retryable: 10-minute session budget cannot elapse in API-functional scope; semantics documented in the skip reason). Gates: gofmt -l clean, go vet ./tests/... clean, go build -p 1 ./... OK, U+FFFD scan clean.

## Acceptance Criteria
- [x] Executable test scripts generated for the api surface from all 27 contracts across all 6 journeys (88 outcomes, 1:1 outcome-to-test-function mapping)
- [x] Scripts compile and pass: gofmt/vet/build/test gates green
- [x] Output directory follows the single-surface convention tests/<journey>/ (SURFACE_KEY '.' resolved to no surface-key layer per .forge/config.yaml surfaces: api)

## Notes
Framework resolved by existing-test reconnaissance (no docs/conventions directory exists): Go testing + gin TestMode + httptest.Server with timeout-bounded http.Client + testify assert/require + certtest in-memory fakes, mirroring internal/cert/web/discovery_handler_test.go style. @feature tag format chosen as '// @feature cert-cloud-discovery-import @api-functional' header comments (no Convention Tags section exists; api.md mandates the @api-functional tag; format adjustable later). Only the upstream EIAM auth middleware is stubbed (401-before-role ordering preserved); role derivation runs through the real CertRoleMiddleware claims mapping and the real RequireRoles guards, so the permission-boundary matrix exercises production authorization logic. Assertion depth: placeholder-fingerprint-backfill 82% and duplicate-concurrent-mapping 85% behavioral; four journeys carry documented partial ASSERTION_DEPTH_EXEMPT headers where transport-decision outcomes (401/403/400/404 envelopes, polling iterations) are themselves the contract output. Contract:Journey ratio is 88/94 by construction (one-test-per-Outcome and exactly-one-smoke-per-journey hard rules dominate the 50/50 advisory at this outcome density); each smoke covers happy path plus at least one error path. Fixture specs consumed: ScanSnapshot/CertReference belongs_to counts, Certificate fingerprint-match constraints, CloudAccount status=active and name=accountKey alignment, DiscoveryImportSession branch states. Coverage self-check: 6/6 api journeys covered, 0 gaps.
