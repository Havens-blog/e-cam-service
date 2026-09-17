---
journey: "first-sync-backfill"
step: 6
step-action: "会话收敛终态并验证 probe 收敛"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
skip_eval: true
---

# Contract: first-sync-backfill / Step 6: 会话收敛终态并验证 probe 收敛

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "本轮导入条目全部处理完成（无未决 pending）；台账与映射可查"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "completed 或 partial_failed"
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "等待会话终态，核对逐项结果与台账/映射完整性，并经引用扫描核对 probe 判定"
- Output: "会话收敛 completed（零失败时）；Items[].result 逐项记录导入条目终态（success/failed），跳过/补建/漂移由轮摘要计数（Skipped/Backfilled/Drifted）承载；台账与映射完整；后续引用扫描 probe 由 diff 收敛 consistent"
- State: "终态二值收敛（completed）；台账/映射行数与判定计数一致"
- Side-effect: "none"

## Outcome "second-run-idempotent-empty"
<!-- source: inferred -->
<!-- reasoning: journey Step 6b + Fact Table CERT_SYNC_JUDGE_FOUR_STATES（已映射指纹全部 Skipped）+ CERT_SYNC_ONE_SHOT_200（无导入条目则不创建导入会话，SessionID 为空串） -->
- Preconditions: "首轮回填完成后无任何云端变化"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "云端实例集与台账指纹及映射完全一致（零增量）"
        prerequisite_entity: "Certificate"
- Input: "再次触发一轮同步"
- Output: "全部实例判定为已同步跳过，零导入条目、零台账/映射写；无新导入会话（SessionID 为空）；轮摘要无失败条目且状态 completed"
- State: "台账/映射行数不变（幂等收敛，只读纪律）"
- Side-effect: "none"

## Outcome "timeout-partial-convergence"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_TIMEOUT（cert_sync_service.go:56,248-259,270-272，整体限时脱离调用方 ctx，到期剩余云/条目记 SESSION_TIMEOUT 静态失败因后收敛返回不悬挂） -->
- Preconditions: "同步整体限时到期且仍有剩余云/条目未处理"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
    state_requirements:
      - description: "同步轮整体限时耗尽（剩余云/条目未判定）"
        prerequisite_entity: "CloudAccount"
- Input: "轮继续收敛（不悬挂、不等待外部信号）"
- Output: "剩余云/条目逐条记 SESSION_TIMEOUT 静态失败因（剩余条目可重跑）；已处理部分照常收敛；轮终态 partial_failed"
- State: "到期不回滚已进行的导入（与发现导入管线先持久化后处理口径一致）"
- Side-effect: "none"

## Journey Invariants
- 同步执行路径只入账不上线：不调用任何云写方法（只读纪律）
- 定时轮任意重复执行结果收敛：不产生重复台账/映射
- 会话 operator 标识来源（scheduler/manual）
