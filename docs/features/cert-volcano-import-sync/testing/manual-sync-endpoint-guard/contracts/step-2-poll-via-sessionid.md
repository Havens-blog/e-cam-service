---
journey: "manual-sync-endpoint-guard"
step: 2
step-action: "凭 sessionId 轮询进度至终态"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/journey.md
skip_eval: true
---

# Contract: manual-sync-endpoint-guard / Step 2: 凭 sessionId 轮询进度至终态

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "手动触发受理成功且摘要 sessionId 非空（本轮产生导入会话）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
- Input: "客户端凭摘要中的 sessionId 调用既有进度轮询端点（GET /api/v1/certs/discovery/import/:sessionId）"
- Output: "sessionId 非空即可续看会话进度直至终态（completed/partial_failed 由 status/finishedAt 可判），不丢结果"
- State: "会话文档状态可查"
- Side-effect: "none"

## Outcome "conflict-no-poll-handle"
<!-- source: inferred -->
<!-- reasoning: journey Step 2b + Fact Table CERT_SYNC_CONFLICT_409（discovery_handler.go:280，冲突响应仅 code/message 结构，无 data 载荷） -->
- Preconditions: "409 冲突路径（未创建新会话）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "冲突响应已返回且在途会话存在"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "客户端检查冲突响应"
- Output: "无新 sessionId 产生（响应无 sessionId 字段），不引导轮询；在途会话进度经其既有 sessionId 继续可查"
- State: "无新会话文档创建"
- Side-effect: "none"

## Outcome "empty-sessionid-no-import-session"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_ONE_SHOT_200（SyncRun.SessionID 无导入条目为空串，cert_sync_service.go:157,301-316）+ CERT_SYNC_RUN_VO_FIELDS（sessionId omitempty）+ IMPORT_SESSION_PERSIST_FIRST（discovery_import_service.go:148-150，空条目集不创建会话） -->
- Preconditions: "本轮零导入条目（全部跳过/空转收敛）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "检查 200 摘要 sessionId 字段"
- Output: "sessionId 为空（不产生导入会话），无可轮询句柄；摘要计数仍完整返回 200"
- State: "无 DiscoveryImportSession 文档创建"
- Side-effect: "none"

## Journey Invariants
- 摘要/冲突/错误响应不携带云侧错误细节与堆栈
- 冲突语义 409（CERT_SYNC_IN_PROGRESS）即时返回，不阻塞排队
