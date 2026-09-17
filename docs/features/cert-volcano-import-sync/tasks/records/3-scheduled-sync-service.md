---
status: "completed"
started: "2026-09-17 10:43"
completed: "2026-09-17 11:26"
time_spent: "~43m"
---

# Task Record: 3 多云定时增量同步服务

## Summary
Multi-cloud scheduled incremental cert sync service (task 3): new internal/cert/service/cert_sync_service.go with read-only CertLibraryLister port (per-cloud cert library listing), volcano shim over the task-1 cloudx adapter, CertSyncService with CAS guard (ErrSyncRunning, aligned to probe probeRunning pattern), six-cloud x active-account enumeration with per-cloud/account failure isolation, incremental judgment against ledger fingerprints + existing mappings (skip / import / backfill / drift), and SyncRun summary with static failure reasons (no cloud error detail leakage). Unimported fingerprints are delegated to the existing discovery-import idempotent pipeline via new ImportFromDiscoverySync exposure (same runImport semantics, synchronous form, no behavior change); drift/backfill use existing Upsert (uk_fp_cloud_account) + FindByCloudCertID uploadedAt-latest semantics - no new idempotency mechanism, no mapping schema change. Overall budget reuses discoveryImportTimeout semantics with timeout reasons recorded, no hang. Wired in module.go (Module.CertSyncSvc) for task-4 scheduler and task-5 manual endpoint. Proposal updated in 5 sections for the AC3 deviation (volcano SDK List has no fingerprint field -> task-1 adapter Lists with per-instance Get internally; skip = no import/ledger write at sync layer, Get cost is adapter-inherent; five-cloud listers when added get full skip-Get benefit).

## Changes

### Files Created
- internal/cert/service/cert_sync_service.go
- internal/cert/service/cert_sync_service_test.go

### Files Modified
- internal/cert/service/discovery_import_service.go
- internal/cert/module.go
- docs/proposals/cert-volcano-import-sync/proposal.md

### Key Decisions
- AC3 corrected per dispatcher design note: volcano SDK CertificateGetInstanceList carries no fingerprint field, so the task-1 adapter's ListCertificates internally calls Get per instance; cost control degrades to the sync judgment layer (mapped instances are skipped = no import action, no ledger/mapping write); test assertion is material-channel GetCertChain call count == 0 for the five-cloud (List-returns-metadata) shape, and volcano Get cost is recorded as an adapter-level constraint; proposal Proposed Solution / Innovation / NFR / Key Risks / Success Criteria updated to match
- Reused the existing discovery-import pipeline for imports by adding ImportFromDiscoverySync (synchronous form of the same runImport; session building extracted into shared helper, zero behavior change to the async path) - operator=scheduler marks source; no new import logic invented
- Backfill/drift handled by direct mappings.Upsert at the judgment layer (existing uk_fp_cloud_account semantics, old drift rows left intact, FindByCloudCertID uploadedAt-latest) instead of routing already-ledgered fingerprints through the pipeline Get - avoids an extra cloud Get per backfill and matches the skip-no-Get constraint
- Empty-fingerprint list metadata (SHA-1 class clouds) judged via mapping channel only: mapped -> skip, unmapped -> import item
- Sync runs detached from caller ctx with discoveryImportTimeout budget (scheduler has no request lifecycle; manual-trigger interruption does not roll back progress, aligned with the pipeline's persist-first semantics); overallTimeout field unexported so package tests can shorten it
- Missing lister for a cloud is a capability gap (debug log + skip), not a failure - today only volcano has a cert library lister; five clouds plug in via the same port
- CertSyncService exposes SyncCertificates (scheduler) + SyncCertificatesManual (manual) sharing one CAS guard so task 4's narrow CertificateSyncer port (SyncCertificates(ctx) (SyncRun, error)) is satisfied and task 5 needs no changes to this file

## Test Results
- **Tests Executed**: Yes
- **Passed**: 274
- **Failed**: 0
- **Coverage**: 94.4%

## Acceptance Criteria
- [x] Enumerate all cert-reachable clouds x active accounts (incl. volcano); single cloud/account failure isolated via errorReason without interrupting the rest (partial_failed terminal semantics)
- [x] Fingerprint not in ledger -> import entry via existing idempotent pipeline (ledger + mapping active), session operator=scheduler
- [x] Fingerprint in ledger with complete mapping -> skip with no Get call (corrected gauge: material-channel GetCertChain count == 0 for List-returns-metadata shape; volcano per-instance Get inside List is an adapter-level constraint, recorded as deviation and proposal updated)
- [x] Missing mapping -> backfill; same cloudCertID new fingerprint (drift) -> new mapping refresh + old mapping retained (FindByCloudCertID returns latest fingerprint)
- [x] Concurrent dual-session import of same fingerprint -> exactly 1 ledger row, both sessions no failed items (ErrDuplicateFingerprint counted as success)
- [x] Empty-ledger first run = full backfill (all listed instances imported with complete mappings)

## Notes
Hard Rules verified: read-only discipline (fake adapter with write methods records zero writes; sync path touches only the read-only lister port + pipeline GetCertChain channel); overall timeout reuses discoveryImportTimeout semantics - test injects a 50ms budget, deadline expiry records SESSION_TIMEOUT reasons and returns without hanging; no mapping schema change and no new idempotency mechanism (existing uk_fingerprint + uk_fp_cloud_account upsert + uploadedAt-latest semantics only). Environment: -race unavailable on this host (no cgo/gcc) - concurrency verified via CAS primitive design and a barrier-gated deterministic race test (stable across repeated runs); gofmt checked via CRLF-stripped temp files (true delta 0 on new files); transient build OOMs during verification matched the known link.exe orphan pattern and were cleared. Coverage figures from go test -coverprofile on internal/cert/service (94.42% for cert_sync_service.go statements; 76.5% package total). Regression: internal/cert/... all 9 packages green (-p 2). ioc/wire graph untouched beyond module field addition (build verified).
