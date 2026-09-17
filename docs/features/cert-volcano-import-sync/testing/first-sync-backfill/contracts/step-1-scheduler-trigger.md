---
journey: "first-sync-backfill"
step: 1
step-action: "天级调度到达触发同步轮"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
skip_eval: true
---

# Contract: first-sync-backfill / Step 1: 天级调度到达触发同步轮

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "调度点 cert:cert-import 已注册（cron 0 1 * * *）；同步服务依赖装配完整（列举端口/账号源/导入管线/台账与映射仓储）；无在途同步轮（CAS 空闲）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "含 volcano 在内的已登记云"
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "调度时钟到达 01:00，cert:cert-import 调度点经 CertificateSyncer 窄端口调用 SyncCertificates 发起本轮多云证书同步"
- Output: "同步轮启动且 CAS 占位成功；枚举与增量判定开始；本轮产生未入账实例时创建导入会话并标识 operator=scheduler"
- State: "CAS 守卫置位至轮结束释放；导入会话（若创建）先持久化（running/pending）再同步执行至终态"
- Side-effect: "对云侧仅只读 List/Get 调用（只读纪律，无任何云写方法）"

## Outcome "already-running-cas-skip"
<!-- source: inferred -->
<!-- reasoning: journey Step 1b + Fact Table CERT_SYNC_CAS_GUARD（cert_sync_service.go:251-255，CompareAndSwap 二次触发返回 ErrSyncRunning）+ JOB_CERT_IMPORT_SPEC（scheduler/jobs.go:179-195，ErrSyncRunning 归一为预期让位静默跳过） -->
- Preconditions: "前一轮同步仍在执行（CAS 占位未释放，定时轮或手动轮任一入口在跑）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "同步服务 CAS 守卫处于置位状态（一轮同步在途）"
        prerequisite_entity: "CloudAccount"
- Input: "调度点再次触发 cert:cert-import"
- Output: "同步服务返回 ErrSyncRunning，调度侧按预期让位：本轮静默跳过不重启不报错，调度循环正常返回"
- State: "在途轮不受影响；无第二会话创建；无失败记录"
- Side-effect: "none"

## Outcome "nil-service-degrade"
<!-- source: inferred -->
<!-- reasoning: journey Step 1c + Fact Table JOB_CERT_IMPORT_SPEC（scheduler/jobs.go:183-187，Sync 未装配记 warn 后本轮空转跳过不 panic） -->
- Preconditions: "调度依赖集中 Sync 窄端口未装配（nil）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "CertJobs.Sync 为 nil（可选端口降级形态）"
        prerequisite_entity: "CloudAccount"
- Input: "调度时钟到达触发 cert:cert-import"
- Output: "记录 warn 日志（sync service not wired）后本轮空转跳过，不 panic，调度任务正常返回"
- State: "无同步会话创建；无台账/映射变更；调度循环继续正常运行"
- Side-effect: "none"

## Journey Invariants
- 同步执行路径只入账不上线：不调用任何云写方法（只读纪律）
- 会话 operator 标识来源（scheduler/manual）
- 定时轮任意重复执行结果收敛：不产生重复台账/映射
