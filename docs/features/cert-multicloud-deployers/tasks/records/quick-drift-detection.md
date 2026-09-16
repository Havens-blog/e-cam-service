---
status: "completed"
started: "2026-09-16 16:18"
completed: "2026-09-16 16:28"
time_spent: "~10m"
---

# Task Record: T-quick-doc-drift Detect Spec Drift

## Summary
Spec drift detection for cert-multicloud-deployers proposal vs actual implementation. Drift-only mode: quick-mode feature has no prd-spec/tech-design, and project-level spec dirs (docs/business-rules/, docs/conventions/) do not exist, so the spec object is the proposal plus code/test evidence. Verified all 6 Success Criteria against implementation and test results: SC1 three-cloud references executable (dfc4624, assessChangeable cloud-channel unconditional, discoveryOnlyClouds removed; AllNineCombos test PASS); SC2 five-method x 9 combo coverage (per-cloud deployer tests in repo, deployer package go test ok 1.811s re-run this task, 18-case three-cloud matrix PASS); SC3 cloud cert IDs written to CloudCertMapping in per-cloud forms (TestExecuteMappingWrittenActivePerCloudForm PASS; Azure KV secret-ID-with-version form per cloudx/azure/cert.go header); SC4 failure compensation (CleanupOrphan double-invocation idempotent, active->orphan enqueue, cleanup queue tests PASS); SC5 verify window reuses ProbeDomains cloud-agnostically (verify_window_service.go:391, PASS); SC6 rollback GetCert three-way validity check before rebind (rollback_service.go:21, precheck/rebind tests PASS). Found 1 text-level drift: Constraints line claims Azure SDK as a new dependency, but implementation deliberately uses net/http direct REST calls (internal/shared/cloudx/azure/cert.go header states no Azure SDK dependency; AWS SDK v2 service/acm and service/cloudfront did land in go.mod). Fixed: annotated the Constraints line in proposal.md and appended a full Drift Verification section (per-item table), committed 3de03ac with [auto-specs] tag. Also verified NFR security claims (Credential.Zeroize in deployer/channel.go; cloud error details normalized to sentinel errors not in responses; per-cloud deployer files with fake tests), CloudFront us-east-1 pinning tests, Out of Scope items not implemented (K8s managed detection unchanged, no auto-renewal, no live verification, no cross-cloud disambiguation with TestCrossCloudMixingRejected as negative proof), and 67/67 live API functional tests per testing/latest.md (commit 6c33529). One non-spec finding logged for the pending T-validate-code task: rollback_service.go:60 comment 'discovery-only 三云无成功项场景天然不触达' predates this feature (4df46cf) and is stale now that three-cloud success items do reach InspectCloudCert; not fixed here since a .go comment belongs to a coding task.

## Changes

### Files Created
无

### Files Modified
- docs/proposals/cert-multicloud-deployers/proposal.md

### Key Decisions
无

## Document Metrics
6/6 SC verified consistent; 1 constraint-line drift found and annotated (Azure SDK -> REST direct); 1 stale code comment logged for T-validate-code; proposal +30/-7 lines

## Referenced Documents
- docs/proposals/cert-multicloud-deployers/proposal.md
- docs/features/cert-multicloud-deployers/manifest.md
- docs/features/cert-multicloud-deployers/testing/latest.md
- internal/cert/service/changelist_generator.go
- internal/cert/module.go
- internal/cert/service/rollback_service.go
- internal/cert/service/verify_window_service.go
- internal/cert/deployer/channel.go
- internal/shared/cloudx/azure/cert.go
- tests/multicloudtest/harness.go

## Review Status
final

## Acceptance Criteria
- [x] Run git diff main...HEAD to narrow feature-scope files (task file discovery strategy)
- [x] Verify all 6 proposal Success Criteria item by item against actual implementation and test results
- [x] Project-level spec dirs checked (docs/business-rules/, docs/conventions/ do not exist in this quick-mode feature - no project-level specs to drift-check)
- [x] Drift found (Azure SDK constraint vs net/http REST implementation) recorded as inline annotation in proposal
- [x] Drift Verification section appended with per-item evidence table
- [x] Out of Scope items confirmed not mixed into implementation
- [x] Spec changes committed with [auto-specs] tag (3de03ac)

## Notes
Evidence: proposal verified against commits 747be05 (huawei), 7348497 (aws), 5cf32ab (azure), dfc4624 (assembly + discoveryOnlyClouds removal), 6c33529 (67/67 run-test record). deployer + cert/service package tests re-run this task: both ok (1.811s / 2.086s). go.mod confirmed: huaweicloud-sdk-go-v3 v0.1.213 as proposal states; aws-sdk-go-v2 v1.41.5 with service/acm v1.38.0 + service/cloudfront v1.60.2; no Azure SDK - cloudx/azure/cert.go is a real REST client (net/http direct, KV 2024-02-01 api-version) by design. git diff main...HEAD empty because quick-mode commits land on main directly; feature scope identified via git log instead (same interpretation as the log-query-optimization T-quick-doc-drift precedent). Manifest task statuses left as-is (pending) matching the established quick-mode convention. Stale rollback_service.go:60 comment deliberately not fixed in this doc task to keep scope clean - flagged for T-validate-code.
