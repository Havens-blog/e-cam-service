---
journey: "manual-scheduler-race"
step: 5
step-action: "收敛核对"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/journey.md
skip_eval: true
---

# Contract: manual-scheduler-race / Step 5: 收敛核对

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "双会话均已收敛终态"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "completed 或 partial_failed（终态）"
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "等待双会话终态，核对台账/映射/失败计数"
- Output: "该指纹台账恰 1 条；双会话均无 failed 条目；映射唯一且指向正确（指向入账证书）"
- State: "收敛形态唯一"
- Side-effect: "none"

## Outcome "poll-progress-via-sessionid"
<!-- source: inferred -->
<!-- reasoning: journey Step 5b + Fact Table SESSION_POLL_ENDPOINT（discovery_handler.go:48,219-226，GET /api/v1/certs/discovery/import/:sessionId 复用既有轮询端点）+ CERT_SYNC_ONE_SHOT_200（sessionId 非空仅当本轮产生导入会话） -->
- Preconditions: "手动触发受理成功且摘要含非空 sessionId（本轮有导入条目）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
- Input: "客户端凭摘要中的 sessionId 调用既有进度轮询端点（GET /api/v1/certs/discovery/import/:sessionId）"
- Output: "sessionId 非空即可续看会话进度直至终态（completed/partial_failed 由 status/finishedAt 可判），不丢结果"
- State: "会话文档可查"
- Side-effect: "none"

## Outcome "operator-attribution-both-rounds"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_OPERATORS（cert_sync_service.go:45-48,236-243，SyncCertificates->scheduler / SyncCertificatesManual->manual）+ journey Step 1/2 expected 的 operator 标识 -->
- Preconditions: "定时轮与手动轮各自完成一轮"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 2
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "核对双会话 operator 标识"
- Output: "调度轮会话 operator=scheduler、手动轮 operator=manual，来源可区分不混淆"
- State: "会话留痕（operator 字段各自承载）"
- Side-effect: "none"

## Journey Invariants
- 竞态撞指纹恒记 success，永不降级失败条目
- 幂等：重复轮结果收敛，不产生重复台账/映射
