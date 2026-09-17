---
journey: "cloud-failure-isolation"
step: 1
step-action: "触发同步轮"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/journey.md
skip_eval: true
---

# Contract: cloud-failure-isolation / Step 1: 触发同步轮

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "CAS 空闲；同步入口可用（定时到达或手动触发）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "触发一轮同步（定时到达或手动入口）"
- Output: "受理并启动同步轮（CAS 占位成功）；本轮产生导入条目时创建同步会话"
- State: "CAS 置位至轮结束释放"
- Side-effect: "对云侧仅只读 List/Get 调用"

## Outcome "guard-released-after-partial-failed"
<!-- source: inferred -->
<!-- reasoning: journey Step 1b + Fact Table CERT_SYNC_CAS_GUARD（cert_sync_service.go:251-255，defer 释放与终态无关——partial_failed 亦正常释放） -->
- Preconditions: "前一轮以 partial_failed 终态结束"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "前一轮同步已收敛 partial_failed 终态"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "再次触发同步"
- Output: "CAS 正常释放，新一轮可启动（失败终态不残留占位）"
- State: "新轮 CAS 置位后正常释放"
- Side-effect: "none"

## Journey Invariants
- 会话终态二值收敛：completed / partial_failed
- 失败可重跑幂等收敛
