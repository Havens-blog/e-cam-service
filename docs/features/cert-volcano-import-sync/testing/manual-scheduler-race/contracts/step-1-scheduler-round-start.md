---
journey: "manual-scheduler-race"
step: 1
step-action: "定时轮正常启动"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/journey.md
skip_eval: true
---

# Contract: manual-scheduler-race / Step 1: 定时轮正常启动

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "调度点 cert:cert-import 已接线；同步服务共享同一 CAS 守卫（调度/手动同源）；CAS 空闲"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "调度时钟到达 01:00 触发 cert:cert-import（经 CertificateSyncer 窄端口调用 SyncCertificates）"
- Output: "CAS 占位成功，同步轮启动；本轮产生导入条目时创建会话并标识 operator=scheduler"
- State: "CAS 置位至轮结束释放"
- Side-effect: "对云侧仅只读 List/Get 调用"

## Outcome "already-running-cas-skip"
<!-- source: inferred -->
<!-- reasoning: journey Step 1b + Fact Table CERT_SYNC_CAS_GUARD（cert_sync_service.go:251-255）+ JOB_CERT_IMPORT_SPEC（scheduler/jobs.go:188-193，ErrSyncRunning 属预期让位） -->
- Preconditions: "前一轮同步 running（CAS 未释放，手动轮或定时轮任一在途）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "同步服务 CAS 守卫处于置位状态"
        prerequisite_entity: "CloudAccount"
- Input: "调度点再次触发"
- Output: "ErrSyncRunning 静默让位跳过（不重启不报错），在途轮不受影响"
- State: "无第二会话创建；无失败记录"
- Side-effect: "none"

## Outcome "cas-release-after-terminal"
<!-- source: inferred -->
<!-- reasoning: journey Step 1d + Fact Table CERT_SYNC_CAS_GUARD（cert_sync_service.go:255，defer s.running.Store(false) 与终态无关，completed/partial_failed 均释放） -->
- Preconditions: "前一轮已收敛终态（completed 或 partial_failed）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "前一轮同步已结束（终态收敛）且 CAS 已释放"
        prerequisite_entity: "CloudAccount"
- Input: "再次触发（调度或手动）"
- Output: "可正常启动新一轮（无死锁、无残留占位）"
- State: "新轮 CAS 重新置位后正常释放"
- Side-effect: "none"

## Journey Invariants
- CAS 守卫保证任一时刻至多一轮同步执行（调度面静默跳过、手动面 409）
- 幂等：重复轮结果收敛，不产生重复台账/映射
