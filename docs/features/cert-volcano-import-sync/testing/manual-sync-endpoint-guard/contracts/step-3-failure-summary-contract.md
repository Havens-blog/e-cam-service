---
journey: "manual-sync-endpoint-guard"
step: 3
step-action: "失败摘要核对"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/journey.md
skip_eval: true
---

# Contract: manual-sync-endpoint-guard / Step 3: 失败摘要核对

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "failure-summary-whitelist"
- Preconditions: "本轮存在枚举/判定层失败（Failures 非空）且导入层零失败（importFailed 为 0）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "本轮同步存在云/账号级失败单元（列举或账号读取失败）"
        prerequisite_entity: "CloudAccount"
- Input: "存在失败条目时检查摘要失败面"
- Output: "failures 仅白名单字段（cloud/accountKey/cloudCertId 可省略 + reason）+静态 reason 文案；无云侧错误细节、无堆栈；status=partial_failed"
- State: "摘要与会话留痕一致"
- Side-effect: "none"

## Outcome "import-failure-decoupled"
<!-- source: inferred -->
<!-- reasoning: journey Step 3b（受理与执行结果解耦）+ Fact Table CERT_SYNC_STATUS_RULE（cert_sync_service.go:319-323）+ IMPORT_ITEM_RESULT_ENUM（domain/discovery_session.go:24-26）；与前一 Outcome 按导入层是否有失败互斥划分 -->
- Preconditions: "触发受理成功但同步执行中出现导入失败条目（importFailed 大于 0，无论枚举层是否亦有失败）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_failed"
      - entity_type: "CertLibraryInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "导入条目中存在处理失败实例（其余可成功）"
        prerequisite_entity: "CertLibraryInstance"
- Input: "核对摘要与会话终态"
- Output: "受理 200 不代表全部导入成功：失败在会话逐项结果（result=failed/errorReason）与终态 partial_failed 中如实呈现；HTTP 层不因执行失败改写为 5xx"
- State: "会话终态 partial_failed"
- Side-effect: "none"

## Outcome "zero-failure-completed-baseline"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_STATUS_RULE（cert_sync_service.go:319-323）+ CERT_SYNC_RUN_VO_FIELDS（failures 空集为空数组形态） -->
- Preconditions: "全程零失败（枚举/判定/导入三层均无失败）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "completed"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "核对摘要终态"
- Output: "status=completed、failures 为空数组、importFailed 为 0（受理成功且全部成功的基线形态）"
- State: "终态 completed"
- Side-effect: "none"

## Journey Invariants
- 摘要/冲突/错误响应不携带云侧错误细节与堆栈
- 手动轮与定时轮共享同一幂等收敛语义（不产生重复台账/映射）
