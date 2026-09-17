---
journey: "manual-sync-endpoint-guard"
step: 1
step-action: "OpsEngineer 手动触发同步"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/journey.md
skip_eval: true
---

# Contract: manual-sync-endpoint-guard / Step 1: OpsEngineer 手动触发同步

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success-200-summary"
- Preconditions: "端点已注册且角色中间件生效；操作者具备 OpsEngineer 角色；CAS 空闲"
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
      - entity_type: "CloudCertMapping"
        min_count: 1
- Input: "以 OpsEngineer 身份 POST /api/v1/certs/discovery/sync（无请求体语义）"
- Output: "200 一次性摘要：sessionId（本轮有导入会话时非空）、status（completed/partial_failed）、startedAt/finishedAt、cloudsScanned/accountsScanned/listed/skipped/backfilled/drifted/imported/importSucceeded/importFailed、failures 数组；同步启动且 operator=manual"
- State: "会话（若有导入条目）operator=manual；HTTP 调用返回即本轮终态"
- Side-effect: "同步执行面（不异步）；对云侧仅只读调用"

## Outcome "conflict-409-running"
<!-- source: inferred -->
<!-- reasoning: journey Step 1b + Fact Table CERT_SYNC_CONFLICT_409（discovery_handler.go:234,279-281，errors.Is ErrSyncRunning -> 409 CERT_SYNC_IN_PROGRESS） -->
- Preconditions: "已有一轮同步 running（CAS 未释放）；请求者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "同步服务 CAS 守卫处于置位状态（在途轮存在）"
        prerequisite_entity: "CloudAccount"
- Input: "经手动入口 POST /api/v1/certs/discovery/sync 再次触发"
- Output: "即时 409 CERT_SYNC_IN_PROGRESS 结构化冲突（非 500、不排队不阻塞）；在途会话不受影响"
- State: "无新会话创建"
- Side-effect: "none"

## Outcome "forbidden-non-opsengineer"
<!-- source: inferred -->
<!-- reasoning: journey Step 1c + 端点注册 RequireRoles(RoleOpsEngineer)（discovery_handler.go:57）+ 既有权限口径事实（四发现端点白名单仅 RoleOpsEngineer；未知显式 cert_role 值=deny 不降级 viewer） -->
- Preconditions: "会话已认证但角色为 viewer/auditor/ops_supervisor/仅能力码账号/未知显式 cert_role"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方已认证但不持有 OpsEngineer 角色"
        prerequisite_entity: "CloudAccount"
- Input: "以非 OpsEngineer 已认证身份 POST /api/v1/certs/discovery/sync"
- Output: "一律 403（未知显式角色 deny 不降级 viewer）；不进入业务逻辑"
- State: "无同步启动"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求无有效认证会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证 POST /api/v1/certs/discovery/sync"
- Output: "401 认证失败响应，先于角色判定，无敏感信息泄露"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "forbidden-no-role-signal"
<!-- source: inferred -->
<!-- reasoning: journey Step 1e + 既有权限口径事实（无信号已认证会话 403，deny 语义） -->
- Preconditions: "已认证会话未声明任何 cert_role 信号"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方已认证但会话无任何角色信号"
        prerequisite_entity: "CloudAccount"
- Input: "经手动入口触发"
- Output: "403（无有效角色即 deny）"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 端点角色边界恒为 OpsEngineer 白名单：其他角色 403、未认证 401
- 冲突语义 409（CERT_SYNC_IN_PROGRESS）即时返回，不阻塞排队
