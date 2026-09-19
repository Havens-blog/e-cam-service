---
status: "completed"
started: "2026-09-19 15:13"
completed: "2026-09-19 15:17"
time_spent: "~4m"
---

# Task Record: T-quick-doc-drift Detect Spec Drift

## Summary
Drift-only consolidate-specs run for cert-volcano-deployer. Project-level spec dirs (docs/business-rules/, docs/conventions/) do not exist -> no drift objects (same precedent as cert-multicloud-deployers / cert-volcano-import-sync); vocabulary index skipped (no knowledge dirs to scan). Verified the three registered deviations against final code anchors in internal/cert/deployer/volcano_deployer.go: (1) UploadCert is unified csv-library main flow (port has no product param) + UploadCertForProduct as ProductAwareUploader optional upgrade (15d6e1e) -> proposal §1 'per-product-library upload' description is a text-level drift, annotated inline and recorded; (2) ErrVolcanoCSVCertNotBindable fail-fast confirmed (lines 130-136/616); (3) GetCert waf/alb/nlb fingerprint fallback via mappings.FindByCloudCertID confirmed (lines 1232-1240) - recorded as implementation refinement consistent with port semantics. Appended 'Drift Verification (2026-09-19, T-quick-doc-drift)' section to docs/proposals/cert-volcano-deployer/proposal.md: 8 Success Criteria all satisfied against commits 830d58f/b0f6800/4826fa0/37fcf7f/15d6e1e/5febc04 (staged suite 7/7 PASS, 0 skipped); 1 drift annotated. No business code touched. Committed e5b841f with [auto-specs] tag.

## Changes

### Files Created
无

### Files Modified
- docs/proposals/cert-volcano-deployer/proposal.md

### Key Decisions
无

## Document Metrics
drift: 1 text-level (UploadCert library-form description, annotated inline + Drift Verification row); SC verified: 8/8 satisfied; deviations cross-checked: 3 (1 drift, 2 consistent/refinement); spec dirs: 0 exist (no integration, no vocabulary index)

## Referenced Documents
- docs/proposals/cert-volcano-deployer/proposal.md
- docs/features/cert-volcano-deployer/tasks/quick-drift-detection.md
- docs/features/cert-volcano-deployer/tasks/records/run-test.md
- internal/cert/deployer/volcano_deployer.go
- internal/cert/deployer/volcano_deployer_test.go
- tests/volcano-cert-replacement/volcano_product_aware_upload_test.go
- docs/proposals/cert-multicloud-deployers/proposal.md

## Review Status
final

## Acceptance Criteria
- [x] Spec drift between project-level specs and current code detected (docs/business-rules + docs/conventions scoped by git diff)
- [x] Registered deviations cross-checked against final implementation and recorded honestly
- [x] Proposal Drift Verification section updated where proposal text diverges from final implementation (no business code changed)
- [x] Changes committed with [auto-specs] tag (e5b841f)

## Notes
git diff main...HEAD returned empty (feature commits already on main) - per task file instruction 'If git diff returns no changes, skip - nothing to drift against' applied to project-level specs; docs/ is gitignored so add -f used. Vocabulary generation (Step 12) skipped: docs/decisions, docs/lessons, docs/business-rules, docs/conventions all absent, nothing to aggregate.
