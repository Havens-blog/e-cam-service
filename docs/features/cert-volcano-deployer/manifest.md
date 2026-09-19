---
feature: "cert-volcano-deployer"
created: "2026-09-19"
status: completed
mode: quick
---

# Feature (Quick): cert-volcano-deployer

<!-- Status flow: tasks -> in-progress -> completed -->

## Documents

| Document | Path |
|----------|------|
| Proposal | ../../proposals/cert-volcano-deployer/proposal.md |

## Tasks

| ID | Title | Status | File |
|----|-------|--------|------|
| 1 | 火山部署器证书库层（UploadCert/GetCert/CleanupOrphan） | pending | tasks/1-upload-get-cleanup.md |
| 2 | 火山部署器绑定层（BindResource/ListReferences × 4 产品） | pending | tasks/2-bind-reference.md |
| 3 | 火山引用扫描适配器（4 产品 → CertReference） | pending | tasks/3-reference-scan-adapter.md |
| 4 | module.go 第 6 云装配 + 变更清单回归 | pending | tasks/4-module-wiring.md |
| 5 | 两段式/验证/回滚/孤儿清理火山接通验证 | pending | tasks/5-execution-closure.md |
| T-clean-code | Simplify and Clean Code | pending | (auto-generated) |
| T-test-gen-journeys | Generate Test Journeys | pending | (auto-generated) |
| T-test-gen-contracts | Generate Test Contracts | pending | (auto-generated) |
| T-test-gen-scripts | Generate API Functional Test Scripts | pending | (auto-generated) |
| T-test-run | Run API Functional Test | pending | (auto-generated) |
| T-quick-doc-drift | Detect Spec Drift | pending | (auto-generated) |
| T-validate-code | Validate Code Quality | pending | (auto-generated) |