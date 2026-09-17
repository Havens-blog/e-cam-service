---
journey: "first-sync-backfill"
step: 3
step-action: "实例清单与台账指纹比对"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
skip_eval: true
---

# Contract: first-sync-backfill / Step 3: 实例清单与台账指纹比对

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "台账为空（首 run 语义）或不含目标实例指纹；列举已返回实例元数据（cloudCertID 与指纹均非空）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "台账（Certificate 集合）为空，全部列举实例指纹未登记"
        prerequisite_entity: "Certificate"
- Input: "判定层以实例指纹查台账（GetByFingerprint）并与现有映射比对"
- Output: "空台账下全部列举实例构成导入条目集（首 run=全量回填，无需特殊代码路径）；每实例一条导入条目（cloud/accountKey/cloudCertID 三元组）"
- State: "判定阶段不写台账/映射；导入条目进入本轮待导入集合"
- Side-effect: "判定层不发起任何材料通道云 Get（比对仅基于列举元数据与台账/映射）"

## Outcome "already-mapped-skip"
<!-- source: inferred -->
<!-- reasoning: journey Step 3b + Fact Table CERT_SYNC_JUDGE_FOUR_STATES（cert_sync_service.go:427-429，映射完整同指纹即 Skipped） -->
- Preconditions: "台账已含某实例指纹且该 (指纹, cloud, accountKey) 映射完整"
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
- Input: "下一轮同步处理该实例"
- Output: "判定为已同步跳过：不发起导入、不写台账/映射（五云口径材料通道 Get 计数为 0；火山口径跳过=不导入，List 内逐实例 Get 为适配器固有成本）"
- State: "Skipped 计数加一；台账/映射行数不变"
- Side-effect: "none"

## Outcome "revoked-filtered"
<!-- source: inferred -->
<!-- reasoning: journey Step 3c + Fact Table VOLCANO_FILTER_TWO_PHASE（cloudx/volcano/cert.go:53-55,259-261，List 阶段前置过滤 revoked/非 Issued）；同步路径下该类实例不进判定层，若经导入管线处理则同样被过滤并留失败痕迹 -->
- Preconditions: "火山侧实例 IsCertificateRevoked=true 或 status 非已签发（Issued）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "列举源含 revoked/非 Issued 状态实例（被适配器过滤）"
        prerequisite_entity: "CloudAccount"
- Input: "同步列举并判定该实例"
- Output: "实例被过滤不进入列举返回集（ErrCertFiltered 哨兵语义），不入账、不产生导入条目；若该实例经导入管线处理（Get 阶段）则条目留失败痕迹（静态 reason）"
- State: "无台账/映射写；无该实例的跳过/导入计数"
- Side-effect: "none"

## Journey Invariants
- 台账按指纹全局唯一：任何轮次/并发路径不产生重复台账记录
- 撤销/非 issued 实例永不入账
