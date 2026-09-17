---
journey: "incremental-skip-drift"
step: 6
step-action: "会话收敛核对"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
skip_eval: true
---

# Contract: incremental-skip-drift / Step 6: 会话收敛核对

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "本轮含跳过/补建/漂移混合场景且全部处理完成"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 2
      - entity_type: "CloudCertMapping"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "completed"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "等待终态核对台账/映射/会话逐项结果"
- Output: "台账新旧指纹各 1 条；映射最新行为最新指纹；Items[].result 含导入条目终态；漂移标注经 Drifted 计数与会话留痕承载；会话收敛无失败（漂移不是失败）"
- State: "终态 completed（零失败时）；台账/映射行数与判定计数一致"
- Side-effect: "none"

## Outcome "drift-event-observable"
<!-- source: inferred -->
<!-- reasoning: journey Step 6b + Fact Table CERT_SYNC_OPERATORS（cert_sync_service.go:45-48,236-243，会话 Operator 来源标识）+ CERT_SYNC_JUDGE_FOUR_STATES（Drifted 计数承载漂移语义） -->
- Preconditions: "本轮含漂移实例"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
      - entity_type: "Certificate"
        min_count: 2
      - entity_type: "CloudCertMapping"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "查看会话逐项结果与轮摘要"
- Output: "该实例漂移/补刷语义可观测（Drifted 计数大于 0；导入条目 mappedCertId 指向新指纹）；operator 标识来源（scheduler 或 manual）"
- State: "会话留痕完整（来源与逐项结果可追溯）"
- Side-effect: "none"

## Outcome "no-failed-entries-after-drift"
<!-- source: inferred -->
<!-- reasoning: journey 不变量「漂移是刷新语义不是失败语义（会话无 failed 条目）」+ Fact Table CERT_SYNC_STATUS_RULE（cert_sync_service.go:319-323） -->
- Preconditions: "本轮仅含漂移/补建/跳过（无云侧失败、无导入失败）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "progress.failed"
            value: "0"
      - entity_type: "CloudCertMapping"
        min_count: 1
- Input: "核对会话失败面"
- Output: "失败计数为 0（ImportFailed=0、Failures 为空、无 failed 条目）；幂等语义不降级失败"
- State: "终态 completed"
- Side-effect: "none"

## Journey Invariants
- 漂移是刷新语义不是失败语义（会话无 failed 条目）
- 任意重复轮幂等：不产生重复台账/映射行
