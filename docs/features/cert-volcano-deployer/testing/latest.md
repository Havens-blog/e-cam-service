# Test Report: cert-volcano-deployer

**Date**: 2026-09-19
**Duration**: ~2m (vet + single-package run, fresh -count=1)

## Summary

| Type  | Total | Pass | Fail | Skip |
|-------|-------|------|------|------|
| api-functional | 7 | 7 | 0 | 0 |
| **All** | **7** | **7** | **0** | **0** |

**Result**: ALL PASS (7/7 test scripts, 0 failed, 0 skipped)

Executed via run-tests skill orchestration. Surface=api (task frontmatter, surface-key "." scalar). Repo has no justfile — per established repo precedent (cert-multicloud-deployers run-test record), the just dev/probe/test/teardown lifecycle maps to direct `go vet` + `go test -count=1 -v ./tests/volcano-cert-replacement/` (hermetic in-process harness: fake volcano SDK stub + in-process gin surface; no external server/DB/auth, so no separate dev/probe lifecycle). OOM-safe single-package run. No -race (host has no cgo/gcc); `go vet ./tests/volcano-cert-replacement/` clean.

Contract inventory (docs/features/cert-volcano-deployer/testing/): 5 journeys, 20 contract files, 62 contract outcomes. Journey-to-script mapping:

| Journey | Scripts | Result |
|---------|---------|--------|
| volcano-two-phase-replacement | TestVolcanoCertReplacement_FullLifecycleSmoke (happy path over CDN/WAF/ALB/NLB) | PASS |
| volcano-bind-failure-compensation | TestVolcanoCertReplacement_BindFailureCompensation | PASS |
| volcano-rollback-verify-window | TestVolcanoCertReplacement_RollbackRestoresOldCertPerProduct; TestVolcanoGetCertFingerprintFallback_NoRecordFailSafe | PASS, PASS |
| volcano-dispatch-visibility | TestVolcanoChannelDispatch_ProductVisibility | PASS |
| volcano-product-aware-upload | TestVolcanoProductAwareUpload_FallbackWithoutPort; TestVolcanoProductAwareUpload_PortDirectedIDSpaceMutex | PASS, PASS |

---

## Confidence Rating

**Level**: LOW (REVIEW)
**Confirmed Fact Ratio**: 0.00 (0 of 126 facts in .forge/fact-table.json are runtime+confirmed; all static/inferred)

Runtime evidence exists: suite green on fresh -count=1 (this run); 185 deployer unit tests + 289 service unit tests previously green (task T-test-gen-scripts era). run-tests does not write runtime facts back to the fact table — informational only, not a gate.

### Verification Summary

| Mark   | Count |
|--------|-------|
| VERIFY | 0 |
| REVIEW | 7 |

---

## Results by Test Case

```
=== RUN   TestVolcanoCertReplacement_FullLifecycleSmoke
--- PASS: TestVolcanoCertReplacement_FullLifecycleSmoke (0.01s)
=== RUN   TestVolcanoCertReplacement_BindFailureCompensation
--- PASS: TestVolcanoCertReplacement_BindFailureCompensation (0.00s)
=== RUN   TestVolcanoCertReplacement_RollbackRestoresOldCertPerProduct
--- PASS: TestVolcanoCertReplacement_RollbackRestoresOldCertPerProduct (0.00s)
=== RUN   TestVolcanoChannelDispatch_ProductVisibility
--- PASS: TestVolcanoChannelDispatch_ProductVisibility (0.00s)
=== RUN   TestVolcanoProductAwareUpload_FallbackWithoutPort
--- PASS: TestVolcanoProductAwareUpload_FallbackWithoutPort (0.00s)
=== RUN   TestVolcanoProductAwareUpload_PortDirectedIDSpaceMutex
--- PASS: TestVolcanoProductAwareUpload_PortDirectedIDSpaceMutex (0.00s)
=== RUN   TestVolcanoGetCertFingerprintFallback_NoRecordFailSafe
--- PASS: TestVolcanoGetCertFingerprintFallback_NoRecordFailSafe (0.00s)
PASS
ok  github.com/Havens-blog/e-cam-service/tests/volcano-cert-replacement 0.270s
```

No t.Skip, no TestMain gating, no expected-failure placeholders in the package (grep-verified). Suite name basis: api-functional/volcano-cert-replacement (all 5 journeys share one staged Go package).

---

## Failed Tests Detail

None.

## Notes

- Contracts carry `skip_eval: true` (quick-mode generation without eval gates) — inherited from T-test-gen-contracts, not a run defect.
- AC "no always-pass mocks" satisfied: scripts exercise real replacement orchestration (scan → confirm → product-aware upload → bind → verify probe → orphan cleanup → rollback) against the fake volcano SDK stub; five-cloud regression intactness is asserted in-suite (five-cloud-regression-intact outcome).
- Host constraints honored: no -race (no cgo), golangci-lint unavailable (go vet clean), single-package run avoids the aggregate OOM pitfall.
