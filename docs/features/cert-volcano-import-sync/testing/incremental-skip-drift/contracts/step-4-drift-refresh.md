---
journey: "incremental-skip-drift"
step: 4
step-action: "换证漂移实例刷新"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
skip_eval: true
---

# Contract: incremental-skip-drift / Step 4: 换证漂移实例刷新

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "火山侧同 (cloud, accountKey, cloudCertID) 内容被云端重签发（新指纹）；台账已有旧指纹及其映射"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "旧指纹（重签发前）"
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "cloudCertId"
            value: "与重签发实例相同"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fingerprint"
            value: "新指纹（重签发后）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "判定层处理该实例（FindByCloudCertID 命中旧映射但指纹不同）"
- Output: "新指纹写入台账（经既有导入管线），并新建映射行（刷新语义）；Drifted 计数加一"
- State: "台账新旧指纹各 1 条；映射含新指纹行；旧映射行保留"
- Side-effect: "none"

## Outcome "duplicate-fingerprint-redirect"
<!-- source: inferred -->
<!-- reasoning: journey Step 4b + Fact Table UK_FINGERPRINT（domain/repository.go:12-14，ErrDuplicateFingerprint）+ CERT_SYNC_JUDGE_FOUR_STATES（cert_sync_service.go:417-419，撞指纹幂等归 success） -->
- Preconditions: "重签发内容与台账另一证书同指纹（新指纹已存在于台账）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 2
        field_constraints:
          - field: "fingerprint"
            value: "其一为新指纹（既有证书）"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "处理该实例"
- Output: "ErrDuplicateFingerprint 幂等转补建映射记 success（导入管线归一语义），不产生第二条台账"
- State: "台账该指纹仍恰 1 条；映射指向既有证书"
- Side-effect: "none"

## Outcome "resigned-then-revoked"
<!-- source: inferred -->
<!-- reasoning: journey Step 4c + Fact Table VOLCANO_FILTER_TWO_PHASE（cloudx/volcano/cert.go:53-55,259-261）+ IMPORT_ITEM_RESULT_ENUM（failed 条目带静态 errorReason） -->
- Preconditions: "重签发后的新指纹实例被撤销（IsCertificateRevoked）或 status 非已签发"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "旧指纹"
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "重签发实例处于 revoked/非 Issued 状态（被过滤或不供导入）"
        prerequisite_entity: "CertLibraryInstance"
- Input: "处理该实例"
- Output: "不入账（适配器两阶段过滤/管线过滤语义）；若经导入管线处理则条目留失败痕迹（静态 reason）；旧指纹映射维持留痕态不删"
- State: "台账无新指纹记录；旧映射行不变"
- Side-effect: "none"

## Journey Invariants
- 映射唯一键=certFingerprint+cloud+accountKey（非 cloudCertID）
- 任意重复轮幂等：不产生重复台账/映射行
- 撤销/非 issued 实例永不入账
