---
journey: "cloud-failure-isolation"
step: 4
step-action: "其余云正常完成"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/journey.md
skip_eval: true
---

# Contract: cloud-failure-isolation / Step 4: 其余云正常完成

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "存在失败单元的同时其余云×账号正常可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "跨两个云（其一含失败单元）"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "同步继续处理其余云×账号"
- Output: "正常单元的导入/跳过/补刷照常收敛，不受失败单元影响（失败不跨单元传播）"
- State: "正常单元计数（Listed/Skipped/Imported 等）如实累计"
- Side-effect: "none"

## Outcome "rerun-backfills-failed-cloud"
<!-- source: inferred -->
<!-- reasoning: journey Step 4b + Fact Table CERT_SYNC_JUDGE_FOUR_STATES（未入账指纹转导入）+ UK_FINGERPRINT（已入账指纹命中即跳过） -->
- Preconditions: "上轮某云列举失败，云端证书仍在且未入账"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "上轮失败面实例本轮列举可达且指纹不在台账"
        prerequisite_entity: "Certificate"
- Input: "重跑一轮同步"
- Output: "幂等可重入：失败面实例本轮入账补齐，成功面不重复处理（已入账指纹跳过）"
- State: "上轮失败面台账无行、本轮新增；上轮成功面行数不变"
- Side-effect: "none"

## Journey Invariants
- 单云单账号失败不中断其他云与账号
- 失败可重跑幂等收敛
