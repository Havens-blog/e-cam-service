---
journey: "incremental-skip-drift"
step: 5
step-action: "旧映射留痕与反查取最新"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
skip_eval: true
---

# Contract: incremental-skip-drift / Step 5: 旧映射留痕与反查取最新

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "换证漂移完成（同 cloudCertID 存在新旧两指纹映射）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 2
        field_constraints:
          - field: "fingerprint"
            value: "新旧指纹各一"
      - entity_type: "CloudCertMapping"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "cloudCertId"
            value: "两行同 cloudCertID"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "核对旧指纹映射与按云证书 ID 反查（FindByCloudCertID）"
- Output: "旧指纹映射留痕不删；反查按 uploadedAt 降序取最新（指向新指纹）；会话记录漂移事件（Drifted 计数）"
- State: "两条映射行并存"
- Side-effect: "none"

## Outcome "multi-history-order"
<!-- source: inferred -->
<!-- reasoning: journey Step 5b + Fact Table MAPPING_REVERSE_LATEST（cert_sync_service.go:30-32，FindByCloudCertID 按 uploadedAt 降序取最新） -->
- Preconditions: "同 cloudCertID 存在多条历史映射（多次换证）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 3
      - entity_type: "CloudCertMapping"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "cloudCertId"
            value: "三行同 cloudCertID"
          - field: "uploadedAt"
            value: "严格递增（多次换证历史）"
- Input: "按云证书 ID 反查"
- Output: "按 uploadedAt 降序取最新一条，指向最新指纹"
- State: "历史行全部保留"
- Side-effect: "none"

## Outcome "old-mapping-not-deleted"
<!-- source: inferred -->
<!-- reasoning: journey Step 5c + Fact Table MAPPING_REVERSE_LATEST（同 cloudCertID 旧指纹映射行不删，漂移留痕） -->
- Preconditions: "换证漂移完成后检查旧映射行"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 2
      - entity_type: "CloudCertMapping"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "检查旧映射行"
- Output: "留痕不删（孤儿回收 Out of Scope，属清理域独立任务）；反查与增量判定不受旧行影响"
- State: "无删除动作发生"
- Side-effect: "none"

## Journey Invariants
- 旧指纹映射只留痕不删；反查恒取 uploadedAt 最新
- 映射唯一键=certFingerprint+cloud+accountKey（非 cloudCertID）
