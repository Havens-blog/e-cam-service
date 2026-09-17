---
journey: "first-sync-backfill"
step: 5
step-action: "建立云证书映射"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
skip_eval: true
---

# Contract: first-sync-backfill / Step 5: 建立云证书映射

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "实例已成功入账（台账含其指纹）；该 (指纹, cloud, accountKey) 尚无映射"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "与列举实例指纹一致"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "该实例指纹尚无 (指纹, cloud, accountKey) 映射行"
        prerequisite_entity: "CloudCertMapping"
- Input: "对成功入账实例按 (指纹, cloud, accountKey) Upsert 建立云证书映射"
- Output: "每个实例一条映射且指向新入账证书（会话条目 mappedCertId 绑定）；映射唯一键 uk_fp_cloud_account 命中即写入"
- State: "CloudCertMapping 新增一行；台账行数不变"
- Side-effect: "none"

## Outcome "rerun-no-duplicate-mapping"
<!-- source: inferred -->
<!-- reasoning: journey Step 5 expected「重复执行不产生重复映射行」+ Fact Table MAPPING_UNIQUE_KEY（cert_sync_service.go:30-32,435-443，Upsert 唯一键幂等） -->
- Preconditions: "同一实例映射已存在且与列举指纹一致（后续轮再次判定）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "重复处理该实例"
- Output: "Upsert 唯一键幂等，不产生第二行映射；判定归入已映射跳过集（Skipped 计数而非重复补建）"
- State: "映射行数不变"
- Side-effect: "none"

## Outcome "mapping-binding-consistency"
<!-- source: inferred -->
<!-- reasoning: journey Step 5 expected「每个实例一条映射且指向新入账证书」+ Fact Table CERT_SYNC_JUDGE_FOUR_STATES（Upsert 字段面：CertFingerprint/Cloud/AccountKey/CloudCertID） -->
- Preconditions: "本轮存在成功入账实例且映射已建立"
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
- Input: "核对映射行与台账证书的绑定关系"
- Output: "映射行 CertFingerprint 与台账证书指纹一致，Cloud/AccountKey/CloudCertID 三元组与来源实例一致"
- State: "映射与台账一对一绑定"
- Side-effect: "none"

## Journey Invariants
- 台账按指纹全局唯一：任何轮次/并发路径不产生重复台账记录
- 定时轮任意重复执行结果收敛：不产生重复台账/映射
