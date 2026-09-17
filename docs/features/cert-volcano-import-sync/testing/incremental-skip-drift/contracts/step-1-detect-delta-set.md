---
journey: "incremental-skip-drift"
step: 1
step-action: "同步轮比对识别增量集"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
skip_eval: true
---

# Contract: incremental-skip-drift / Step 1: 同步轮比对识别增量集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "台账已含若干实例指纹与映射（非首轮）；列举已返回实例元数据；CAS 空闲"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
      - entity_type: "CertLibraryInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
- Input: "触发一轮同步（定时或手动），判定层以实例清单元数据 vs 台账指纹+现有映射比对"
- Output: "仅「指纹不在台账」或「映射缺失/漂移」实例进入导入/补刷动作，其余归入跳过集；判定四态计数（Skipped/Backfilled/Drifted/Imported）如实反映增量构成"
- State: "判定阶段无台账/映射写；动作条目在后续步骤落地"
- Side-effect: "判定层不发起任何材料通道云 Get（比对仅基于列举元数据与台账/映射）"

## Outcome "all-mapped-zero-action"
<!-- source: inferred -->
<!-- reasoning: journey Step 1b + Fact Table CERT_SYNC_JUDGE_FOUR_STATES（已映射指纹全部 Skipped）+ IMPORT_SESSION_PERSIST_FIRST（discovery_import_service.go:148-150，空条目集不创建导入会话） -->
- Preconditions: "全部列举实例已映射（无任何增量）"
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
      - description: "云端实例与台账指纹及映射完全对齐（零增量）"
        prerequisite_entity: "Certificate"
- Input: "一轮同步"
- Output: "零导入动作、零台账/映射写；五云口径零材料通道 Get；轮摘要 Imported=0 且无导入会话创建（SessionID 为空）"
- State: "台账/映射不变"
- Side-effect: "none"

## Outcome "empty-fingerprint-degraded"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_EMPTY_FINGERPRINT_DEGRADED（cert_sync_service.go:399-410，指纹为空降级形态：已映射即跳过、无映射转导入） -->
- Preconditions: "某实例列举层无法复核指纹（指纹为空的降级形态，如 SHA-1 口径列举源）"
  fixture_spec:
    entities:
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fingerprint"
            value: "空串（列举层无法复核指纹）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "判定层处理该实例"
- Output: "已映射即视为收敛跳过；无映射则转导入条目（管线解析真实指纹后补建映射）"
- State: "降级实例不产生误判失败"
- Side-effect: "none"

## Journey Invariants
- 已映射指纹跳过：判定层不发起导入动作、不写台账/映射
- 任意重复轮幂等：不产生重复台账/映射行
