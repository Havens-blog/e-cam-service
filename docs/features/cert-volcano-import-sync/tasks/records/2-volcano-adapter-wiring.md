---
status: "completed"
started: "2026-09-17 10:19"
completed: "2026-09-17 10:41"
time_spent: "~22m"
---

# Task Record: 2 火山发现适配器包装与模块装配

## Summary
Wired the task-1 volcano cert library adapter into the discovery import pipeline: added service.NewVolcanoDiscoveryCertAdapter port wrapper (reuses the existing discoveryCertAdapter shim, compile-time DiscoveryCertAdapter assertion, pure mapping helper volcanoCertMaterial) and registered it in internal/cert/module.go's discovery adapter list alongside the five existing clouds, sharing logger/credential assembly. Cloud identity translated from shared/domain CloudProviderVolcano since cert/domain.Cloud has no volcano constant (domain package off-limits per Hard Rule).

## Changes

### Files Created
- internal/cert/service/discovery_adapter_volcano.go
- internal/cert/service/discovery_adapter_volcano_test.go

### Files Modified
- internal/cert/module.go

### Key Decisions
- Wrapper kept in new file discovery_adapter_volcano.go (Hard Rule primary name) instead of appending to discovery_import_service.go where the five inline wrappers live
- Cloud identity: const discoveryCloudVolcano = domain.Cloud(sharedomain.CloudProviderVolcano) ('volcano'); cert/domain.Cloud enum untouched per Hard Rule file restriction
- Port mapping extracted into pure helper volcanoCertMaterial(inst, err) so both branches are unit-testable without SDK injection (volcano.CertAdapter client factory is unexported, not fakeable from service package)
- ErrCertFiltered passes through unmodified: service layer records generic CERT_GET_FAILED; filtered/gone semantics belong to the task-3 sync service which consumes the cloudx adapter directly
- module.go wiring comment updated from five-cloud to six-cloud wording for doc accuracy

## Test Results
- **Tests Executed**: Yes
- **Passed**: 301
- **Failed**: 0
- **Coverage**: 100.0%

## Acceptance Criteria
- [x] NewVolcanoDiscoveryCertAdapter returns DiscoveryCertAdapter implementation (compile-time interface assertion)
- [x] module.go discovery import adapter list includes the volcano entry alongside the five clouds, sharing CertAdapter/credential assembly
- [x] Existing five-cloud discovery import unit tests and manual import full-chain tests all green (new wiring breaks nothing)

## Notes
Coverage 100.0% = new wrapper file (go tool cover -func, both functions); service package total 76.3% is pre-existing baseline, stopped adding tests at target. Test counts: service 261 + cert root 6 + volcano 34, all -count=1, no -race (host has no cgo). Lint via go vet ./internal/cert/... clean (staticcheck unavailable on host). go build -p 1 ./... exit 0. gofmt true-delta 0 bytes on all three touched files (CRLF-aware tr check). One transient host OOM during first test compile, passed on retry; 3 elevated orphan link.exe processes from other builds could not be killed (access denied), non-blocking.
