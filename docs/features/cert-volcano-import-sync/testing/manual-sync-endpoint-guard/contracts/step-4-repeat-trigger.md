---
journey: "manual-sync-endpoint-guard"
step: 4
step-action: "空闲期重复手动触发"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/journey.md
skip_eval: true
---

# Contract: manual-sync-endpoint-guard / Step 4: 空闲期重复手动触发

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success-repeat-with-new-instances"
- Preconditions: "前轮终态后 CAS 已释放；云端存在新增（未入账）证书"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "前轮已收敛且本轮存在未入账增量"
        prerequisite_entity: "Certificate"
- Input: "前轮终态后再次以 OpsEngineer 身份手动触发"
- Output: "再次 200 受理并启动新一轮；新增实例入账；幂等收敛（不产生重复台账/映射）"
- State: "新会话（若有导入条目）；CAS 重新占位并正常释放"
- Side-effect: "none"

## Outcome "repeat-zero-delta-converged"
<!-- source: inferred -->
<!-- reasoning: journey Step 4 expected 幂等收敛 + Fact Table CERT_SYNC_ONE_SHOT_200（零新条目亦 200）+ IMPORT_SESSION_PERSIST_FIRST（discovery_import_service.go:148-150，空条目集不创建会话） -->
- Preconditions: "前轮已收敛且云端与台账无任何差异（零增量）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
    state_requirements:
      - description: "云端实例与台账指纹及映射完全对齐（零增量）"
        prerequisite_entity: "Certificate"
- Input: "再次手动触发"
- Output: "200 且 imported 为 0、sessionId 为空（不创建导入会话）、台账/映射零写（空转收敛非错误）"
- State: "台账/映射行数不变"
- Side-effect: "none"

## Outcome "service-not-wired-500"
<!-- source: inferred -->
<!-- reasoning: journey Step 4b + Fact Table CERT_SYNC_NOT_WIRED_500（discovery_handler.go:272-275，h.sync==nil -> 500 INTERNAL_ERROR 'cert sync service not wired'） -->
- Preconditions: "同步服务依赖异常（handler 未装配 sync 依赖）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "DiscoveryHandler 构造未注入 CertSyncService（可选变参零形态）"
        prerequisite_entity: "CloudAccount"
- Input: "经手动入口触发"
- Output: "结构化错误响应 500 INTERNAL_ERROR 'cert sync service not wired'（非泄露堆栈），服务不 panic"
- State: "无同步启动"
- Side-effect: "none"

## Journey Invariants
- 手动轮与定时轮共享同一幂等收敛语义（不产生重复台账/映射）
- 摘要/冲突/错误响应不携带云侧错误细节与堆栈
