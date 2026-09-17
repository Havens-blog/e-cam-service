---
journey: "incremental-skip-drift"
step: 2
step-action: "已映射实例跳过"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
skip_eval: true
---

# Contract: incremental-skip-drift / Step 2: 已映射实例跳过

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "实例指纹在台账且该 (指纹, cloud, accountKey) 映射完整且同指纹"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "与列举实例指纹一致"
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "判定层处理已映射指纹实例"
- Output: "跳过：无导入动作、无台账/映射写；Skipped 计数加一"
- State: "台账/映射行数不变"
- Side-effect: "none"

## Outcome "volcano-skip-semantics"
<!-- source: inferred -->
<!-- reasoning: journey Step 2b + 服务增量判定口径注释（cert_sync_service.go:34-38，火山 SDK List 无指纹字段故 List 内逐实例 Get 为适配器固有成本，跳过=不产生导入动作与台账写） -->
- Preconditions: "火山实例已入账且映射完整"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "一轮同步处理该实例"
- Output: "跳过=不导入不写台账/映射；不作跳过收益断言口径（List 内逐实例 Get 为适配器固有成本，不属于跳过语义范畴）"
- State: "台账/映射行数不变；Skipped 计数加一"
- Side-effect: "none"

## Outcome "skip-not-failure"
<!-- source: inferred -->
<!-- reasoning: journey 不变量「漂移是刷新语义不是失败语义」+ Fact Table CERT_SYNC_STATUS_RULE（cert_sync_service.go:319-323，仅 ImportFailed>0 或 Failures 非空才 partial_failed） -->
- Preconditions: "存在任意数量已映射实例被跳过（本轮无其他失败面）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "轮收敛核对"
- Output: "跳过不计入失败（ImportFailed=0、Failures 为空、无 failed 条目）；全部跳过时轮终态仍 completed"
- State: "终态 completed"
- Side-effect: "none"

## Journey Invariants
- 已映射指纹跳过：判定层不发起导入动作、不写台账/映射
- 漂移是刷新语义不是失败语义（会话无 failed 条目）
