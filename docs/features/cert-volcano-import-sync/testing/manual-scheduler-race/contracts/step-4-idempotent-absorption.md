---
journey: "manual-scheduler-race"
step: 4
step-action: "后到者幂等消化"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/journey.md
skip_eval: true
---

# Contract: manual-scheduler-race / Step 4: 后到者幂等消化

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success-duplicate-redirect"
- Preconditions: "后到者写入台账时捕获指纹唯一键冲突（ErrDuplicateFingerprint）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "先到轮已入账的同指纹证书"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "后到轮导入条目触发 uk_fingerprint 唯一冲突"
        prerequisite_entity: "Certificate"
- Input: "后到者处理该实例"
- Output: "转取既有证书补建映射，条目记 success（幂等语义，不算失败）"
- State: "台账仍 1 条；映射补建指向既有证书"
- Side-effect: "none"

## Outcome "both-sessions-success-no-failed"
<!-- source: inferred -->
<!-- reasoning: journey Step 4b + Fact Table IMPORT_ITEM_RESULT_ENUM（domain/discovery_session.go:24-26）+ CERT_SYNC_STATUS_RULE（cert_sync_service.go:319-323，幂等归 success 不产生 failed） -->
- Preconditions: "竞态窗口内双写入完成"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 2
      - entity_type: "Certificate"
        min_count: 1
- Input: "核对双会话逐项结果"
- Output: "双方条目均 success，失败计数为 0（幂等语义不降级失败）"
- State: "双会话终态无 failed 条目"
- Side-effect: "none"

## Outcome "no-second-ledger-row"
<!-- source: inferred -->
<!-- reasoning: journey Step 5 expected「该指纹台账恰 1 条」+ Fact Table UK_FINGERPRINT（repository/certificate.go:26，唯一冲突返回 ErrDuplicateFingerprint 不产生第二行） -->
- Preconditions: "同指纹先后两轮均尝试入账"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "核对台账"
- Output: "台账该指纹恰 1 条（唯一键约束构造性保证）；后到者不重复拉链解析入账"
- State: "台账行数不随竞态增长"
- Side-effect: "none"

## Journey Invariants
- 竞态撞指纹恒记 success，永不降级失败条目
- 台账指纹全局唯一：竞态双方不产生第二条同指纹台账
