---
status: "completed"
started: "2026-09-17 12:44"
completed: "2026-09-17 13:04"
time_spent: "~20m"
---

# Task Record: T-test-gen-contracts Generate Test Contracts

## Summary
gen-contracts 完成：5 journeys 全部生成 Contract 规格（26 文件 / 77 outcomes），六维语义描述符 + fixture_spec + 技术锚点空缺降级（无 handbook），fact-table 合并 30 条 CERT_SYNC_* 静态事实，密度全部 ON_TARGET，schema 校验零错误

## Changes

### Files Created
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/step-1-scheduler-trigger.md
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/step-2-enumerate-clouds.md
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/step-3-fingerprint-compare.md
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/step-4-import-unlisted.md
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/step-5-establish-mapping.md
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/step-6-session-convergence.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/step-1-detect-delta-set.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/step-2-mapped-skip.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/step-3-backfill-mapping.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/step-4-drift-refresh.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/step-5-old-mapping-reverse-lookup.md
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/step-6-session-reconcile.md
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/step-1-scheduler-round-start.md
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/step-2-manual-trigger.md
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/step-3-overlapping-processing.md
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/step-4-idempotent-absorption.md
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/step-5-race-convergence.md
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/step-1-trigger-round.md
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/step-2-enumerate-units.md
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/step-3-isolated-unit-failure.md
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/step-4-remaining-clouds-complete.md
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/step-5-terminal-and-rerun.md
- docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/contracts/step-1-manual-trigger-contract.md
- docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/contracts/step-2-poll-via-sessionid.md
- docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/contracts/step-3-failure-summary-contract.md
- docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/contracts/step-4-repeat-trigger.md

### Files Modified
- .forge/fact-table.json
- docs/features/cert-volcano-import-sync/tasks/index.json

### Key Decisions
无

## Cases Generated
77

## Cases Evaluated
77

## Scripts Created
- docs/features/cert-volcano-import-sync/testing/first-sync-backfill/contracts/
- docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/contracts/
- docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/contracts/
- docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/contracts/
- docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/contracts/

## Test Results
26 contracts (first-sync-backfill 6 / incremental-skip-drift 6 / manual-scheduler-race 5 / cloud-failure-isolation 5 / manual-sync-endpoint-guard 4), 77 outcomes; schema validation 0 errors; density all ON_TARGET (High 3-5/step 13-20 total, Medium 2-3/step 8-12 total)

## Acceptance Criteria
- [x] At least 1 Contract file generated per Journey
- [x] Each Contract has six-dimension declarations with semantic descriptors (no regex)
- [x] Risk-driven Outcome density targets met per Journey risk level
- [x] Fact Table written to .forge/fact-table.json
- [x] All Contracts passed schema validation

## Notes
SKIP_EVAL_GATE=true（quick 模式）：跳过 eval-journey 前置，全部契约带 skip_eval: true 头。离散契约覆盖任务提示要求的 sync 端点 200 摘要 / 409 CERT_SYNC_IN_PROGRESS / 401/403 鉴权（manual-sync-endpoint-guard step-1 5 outcomes + manual-scheduler-race step-2 unauthorized surface-required）。fact-table.json 按规则合并（60 存量 + 30 新增 = 90 条，source=static/confidence=inferred），顺手修复既有 IMPORT_VALIDATION_ERRORS 条目 U+FFFD（单字箭头按 discovery_handler.go:180-181 权威旁证重建）。Preconditions 互斥按 importFailed=0/>0、零增量/有增量等划分显式声明。下游 gen-test-scripts 注意：surface=api 单表面（无 surface-key 层），POST /certs/discovery/sync 同步执行面（返回即终态），五云口径跳过断言材料通道 Get 计数 0、火山口径跳过=不导入不写。提交 42b983e（forge feature set 后链式提交，仅本 feature 文件）。
