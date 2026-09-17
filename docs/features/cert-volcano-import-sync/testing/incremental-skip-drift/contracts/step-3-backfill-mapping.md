---
journey: "incremental-skip-drift"
step: 3
step-action: "映射缺失实例补建"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/incremental-skip-drift/journey.md
skip_eval: true
---

# Contract: incremental-skip-drift / Step 3: 映射缺失实例补建

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "台账有实例指纹但该 (指纹, cloud, accountKey) 映射缺失（漂移态）"
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
      - description: "该 (指纹, cloud, accountKey) 无映射行"
        prerequisite_entity: "CloudCertMapping"
- Input: "判定层处理该实例（指纹已命中台账但映射缺失）"
- Output: "仅补建映射（Upsert）记 Backfilled 计数（success 语义）；台账不重复（指纹已命中不再入账）"
- State: "CloudCertMapping 新增一行；台账行数不变"
- Side-effect: "none"

## Outcome "unique-key-replay"
<!-- source: inferred -->
<!-- reasoning: journey Step 3b + Fact Table MAPPING_UNIQUE_KEY（cert_sync_service.go:30-32,435-443，uk_fp_cloud_account Upsert 唯一键幂等） -->
- Preconditions: "映射已补建后重放同实例"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "再处理同实例"
- Output: "唯一键幂等不产生第二行；此后判定归入已映射跳过集（Backfilled 不再累计）"
- State: "映射行数不变"
- Side-effect: "none"

## Outcome "upsert-failed-static-reason"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_STATIC_REASONS（reasonSyncMappingFailed「INTERNAL_ERROR: 映射补建失败」，cert_sync_service.go:55,440-442） -->
- Preconditions: "映射 Upsert 写库失败（仓储层错误）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "映射仓储写入通道异常（Upsert 返回错误）"
        prerequisite_entity: "CloudCertMapping"
- Input: "判定层尝试补建映射"
- Output: "该实例记「INTERNAL_ERROR: 映射补建失败」静态失败因（仓储/云侧细节仅日志）；其余实例继续；轮终态 partial_failed"
- State: "无映射写入；Failures 增加一条（cloud/accountKey/cloudCertID 定位）"
- Side-effect: "云侧与仓储错误细节仅进服务层日志"

## Journey Invariants
- 任意重复轮幂等：不产生重复台账/映射行
- 单实例失败隔离不中断（失败记静态文案后其余照常收敛）
