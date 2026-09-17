---
status: "completed"
started: "2026-09-17 12:32"
completed: "2026-09-17 12:42"
time_spent: "~10m"
---

# Task Record: T-test-gen-journeys Generate Test Journeys

## Summary
Generated 5 test Journey documents for cert-volcano-import-sync via /gen-journeys (Proposal/quick mode; forge surfaces=api single surface). Golden Path: first-sync-backfill (High, 6 steps, complex-feature classification). Journeys: first-sync-backfill (first run full backfill + happy path convergence, High), incremental-skip-drift (mapped-fingerprint skip + mapping backfill + renewal drift refresh/retain + latest-by-uploadedAt reverse lookup, High), manual-scheduler-race (CAS guard + manual 409 + ErrDuplicateFingerprint idempotent success, High), cloud-failure-isolation (per-cloud per-account isolation, partial_failed, empty/unreachable account skip, Medium), manual-sync-endpoint-guard (POST /api/v1/certs/discovery/sync contract: 200 summary, 409 CERT_SYNC_IN_PROGRESS, OpsEngineer boundary 401/403, sessionId poll reuse, High). 26 happy-path steps, 37 edge cases, 26 journey invariants total. Validation passed: names, risk levels, surface coverage (api), high-risk edge density (edge>=steps: 8>=6, 8>=6, 7>=5, 7>=4), golden path >=5 steps with domain terminology, every step has User Action + Expected Result, proposal traceability in every frontmatter. U+FFFD scan performed post-write; 2 corrupted CJK chars found and reconstructed (cloud-failure-isolation, manual-sync-endpoint-guard).

## Changes

### Files Created
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/journey.md
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/journey.md
- docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/journey.md

### Files Modified
无

### Key Decisions
无

## Cases Generated
63

## Cases Evaluated
N/A

## Scripts Created
无

## Test Results
5 journeys generated (26 happy-path steps + 37 edge cases = 63 cases; 26 invariants). Static validation script passed for all 5 files: frontmatter fields, risk validity, surface_types/surface_keys coverage of configured api surface, High-risk edge density, golden-path dual constraints, step action/result completeness. No code reconnaissance performed (narrative extraction only); cloud side to be faked/stubbed by downstream gen-test-scripts per project convention (no real Volcano account).

## Acceptance Criteria
- [x] At least 1 Journey file generated under docs/features/cert-volcano-import-sync/testing/
- [x] Each Journey has: name, risk level, happy path steps, edge cases, invariants
- [x] High-risk Journeys have edge case count >= happy path step count
- [x] All Journey files committed (AUTO_COMMIT=true)

## Notes
Mode detection: prd/ dir empty -> Proposal Mode (quick). Minimum information check passed: proposal.md has Scope + Success Criteria + Key Scenarios (quality notice not required). AUTO_COMMIT=true from task context: user review skipped, Step 5 validation executed and passed before commit. Journeys follow the cert-cloud-discovery-import precedent format (frontmatter with golden_path/surface_types/surface_keys/sources). Scenario 4+5 (empty account / cloud API failure) merged into cloud-failure-isolation per merge-related-stories rule; manual endpoint contract carried by manual-sync-endpoint-guard. No overlap with parallel log-query-optimization session files (testing/ dir is feature-scoped docs only).
