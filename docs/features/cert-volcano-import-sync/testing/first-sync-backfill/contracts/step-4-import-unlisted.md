---
journey: "first-sync-backfill"
step: 4
step-action: "未入账实例拉链解析入账"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/first-sync-backfill/journey.md
skip_eval: true
---

# Contract: first-sync-backfill / Step 4: 未入账实例拉链解析入账

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "增量集含未入账指纹实例（指纹不在台账）；导入管线可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "台账不含这些实例的指纹"
        prerequisite_entity: "Certificate"
- Input: "未入账实例导入条目经 ImportFromDiscoverySync 进入既有发现导入幂等管线（拉链解析，指纹 sha256/SAN/有效期口径与 CAS 一致）"
- Output: "台账新增证书记录（沿既有发现导入管线）；会话条目 result=success 且 mappedCertId 指向新入账证书"
- State: "DiscoveryImportSession 先持久化（running/pending）后同步处理至终态；台账新增记录"
- Side-effect: "同步执行面——调用返回即本轮终态；对云侧仅经 GetCertChain 只读读通道拉链"

## Outcome "cross-cloud-same-fingerprint"
<!-- source: inferred -->
<!-- reasoning: journey Step 4b + Fact Table UK_FINGERPRINT（domain/repository.go:12-14）+ CERT_SYNC_JUDGE_FOUR_STATES（cert_sync_service.go:417-419，同指纹跨云/跨账号多三元组各自成条目，ErrDuplicateFingerprint 幂等归 success） -->
- Preconditions: "两云各有一张内容相同（同指纹）的证书进入增量集"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "两个不同云"
      - entity_type: "CertLibraryInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fingerprint"
            value: "两实例指纹相同"
    state_requirements:
      - description: "台账为空（两实例均未入账）"
        prerequisite_entity: "Certificate"
- Input: "同一轮先后处理两实例"
- Output: "台账恰 1 条（先到者入账，后到者撞指纹唯一键幂等转补建映射）；两云各自账号各建映射；两实例会话条目均 success 不记失败"
- State: "无重复台账行；映射两行分属两云账号"
- Side-effect: "none"

## Outcome "chain-fetch-failed"
<!-- source: inferred -->
<!-- reasoning: journey Step 4c + Fact Table IMPORT_ITEM_RESULT_ENUM（domain/discovery_session.go:24-26，failed 条目带静态 errorReason）+ 管线单条失败/panic 隔离语义（discovery_import_service.go:178-184） -->
- Preconditions: "某实例拉链时云 API 失败（导入管线单条处理失败）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertLibraryInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "其一实例材料通道拉链失败，其余实例正常"
        prerequisite_entity: "CertLibraryInstance"
- Input: "同步继续处理后续实例"
- Output: "失败实例会话条目 result=failed 且 errorReason 为静态文案（不含云侧错误细节）；其余实例照常收敛；轮终态按失败语义收敛 partial_failed"
- State: "单条失败不中断会话其余条目处理"
- Side-effect: "云侧错误细节仅进服务层日志"

## Journey Invariants
- 台账按指纹全局唯一：任何轮次/并发路径不产生重复台账记录
- 单实例/单云失败隔离，不中断其他云与账号
- 撤销/非 issued 实例永不入账
