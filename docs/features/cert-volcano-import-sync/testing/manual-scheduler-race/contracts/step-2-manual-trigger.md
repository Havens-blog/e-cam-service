---
journey: "manual-scheduler-race"
step: 2
step-action: "空闲期手动触发同步"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/manual-scheduler-race/journey.md
skip_eval: true
---

# Contract: manual-scheduler-race / Step 2: 空闲期手动触发同步

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "CAS 空闲（无在途轮）；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "请求方持有 OpsEngineer 角色的有效认证会话"
        prerequisite_entity: "CloudAccount"
- Input: "以 OpsEngineer 身份 POST /api/v1/certs/discovery/sync"
- Output: "200 一次性终态摘要：sessionId（本轮有导入会话时非空）、status、startedAt/finishedAt、cloudsScanned/accountsScanned/listed/skipped/backfilled/drifted/imported/importSucceeded/importFailed、failures 数组；同步启动且会话 operator=manual"
- State: "会话（若创建导入会话）operator=manual；CAS 占位期间拒绝并发轮"
- Side-effect: "同步执行面——HTTP 调用返回即本轮终态"

## Outcome "conflict-409"
<!-- source: inferred -->
<!-- reasoning: journey Step 1c/5c + Fact Table CERT_SYNC_CONFLICT_409（discovery_handler.go:234,279-281，errors.Is ErrSyncRunning -> 409 CERT_SYNC_IN_PROGRESS）+ CERT_SYNC_CAS_GUARD；1c（即时冲突）与 5c（冲突响应体边界）同一前置态，按互斥规则合并声明 -->
- Preconditions: "已有一轮同步 running（CAS 未释放）；请求方具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "同步服务 CAS 守卫处于置位状态（在途轮存在）"
        prerequisite_entity: "CloudAccount"
- Input: "经手动入口 POST /api/v1/certs/discovery/sync 再次触发"
- Output: "即时 409 CERT_SYNC_IN_PROGRESS 结构化冲突（非 500、不排队不阻塞）：响应仅冲突语义，无云侧错误细节、无堆栈、无新 sessionId、不引导轮询"
- State: "无新会话创建；在途会话不受影响，进度经其既有 sessionId 继续可查"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效认证会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证 POST /api/v1/certs/discovery/sync"
- Output: "401 认证失败响应，不进入业务逻辑，无敏感信息泄露"
- State: "无状态变更、无同步启动"
- Side-effect: "none"

## Journey Invariants
- CAS 守卫保证任一时刻至多一轮同步执行（调度面静默跳过、手动面 409）
- 冲突/失败响应不携带云侧错误细节
