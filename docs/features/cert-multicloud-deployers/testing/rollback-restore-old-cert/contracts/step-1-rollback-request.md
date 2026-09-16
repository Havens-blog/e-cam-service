---
journey: "rollback-restore-old-cert"
step: 1
step-action: "对已完成替换的条目发起回滚"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/rollback-restore-old-cert/journey.md
skip_eval: true
---

# Contract: rollback-restore-old-cert / Step 1: 对已完成替换的条目发起回滚

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "rollback-accepted"
- Preconditions: "变更单处于可回滚收敛态（executing 且存在失败条目且无在途条目、或部分完成 partial_completed）；请求条目 ID 指向 status=success 的条目；操作者具备 ops_engineer 角色"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "executing（含失败条目）或 partial_completed"
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "success（已替换完成、持有旧云证书 ID）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "POST /api/v1/certs/changes/:id/rollback 携带条目 ID 列表（非空）"
- Output: "200 受理（确认型 VO）：系统定位映射中记录的旧云证书 ID，并按云解析（SCM ID / ACM ARN / KV secret ID 引用），准备反绑"
- State: "回滚编排启动（入口校验 → 范围解析 → 预检 → 逐条回滚 → 收敛）；条目状态将由 rolled_back/rollback_failed 收敛"
- Side-effect: "后续逐条云回读/绑定调用（异步）"

## Outcome "entry-state-invalid"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_ROLLBACK_ENTRY（rollback_service.go:246-270：executing 需≥1 失败条目且 0 在途，其余状态 InvalidTransitionError）→ 409 CHANGE_STATE_CONFLICT（web/response.go:168-174） -->
- Preconditions: "变更单状态不满足回滚入口校验（如 verifying/completed 等非收敛态，或 executing 但无失败条目、仍有在途条目）"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "verifying 或无失败条目的 executing 等不可回滚态"
      - entity_type: "ChangeItem"
        min_count: 1
- Input: "对该变更单发起回滚（携带非空条目列表）"
- Output: "显式拒绝：409 CHANGE_STATE_CONFLICT（状态机白名单拒绝），不产生任何回滚副作用"
- State: "变更单与条目状态均不变"
- Side-effect: "none"

## Outcome "scope-empty-rejected"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_ROLLBACK_SCOPE（web/change_handler.go:101-104：itemIds 必填非空 → 400 INVALID_REQUEST；rollback_service.go:276-301：范围仅准入 status=success 条目，无可回滚 → ErrRollbackScopeInvalid → 400） -->
- Preconditions: "请求条目列表为空，或所选条目均非 success（如已 rolled_back）导致无可回滚范围"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_completed（条目均已回滚）"
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "rolled_back"
- Input: "提交空条目列表或全为已回滚条目的回滚请求"
- Output: "400 INVALID_REQUEST（回滚请求无可回滚成功条目），不产生回滚副作用"
- State: "无状态变化"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效会话"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
    state_requirements:
      - description: "无有效会话"
        prerequisite_entity: "ChangeOrder"
- Input: "POST /api/v1/certs/changes/:id/rollback 无有效会话调用"
- Output: "HTTP 401 全局认证失败文案（非 cert 模块 Envelope）"
- State: "无状态变化，回滚编排不启动"
- Side-effect: "none"

## Journey Invariants
- 回滚前必须经 GetCert 校验旧云证书有效，禁止在未知状态下盲绑
- 回滚幂等：重复回滚不产生重复副作用，终态唯一（资源引用旧证书）
- 回滚与清理/orphan 状态互斥协调，不允许出现引用已删除证书的终态

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeOrder"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "rollback-accepted: executing/partial_completed；entry-invalid: verifying 等；scope-empty: partial_completed 全回滚"
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "rollback-accepted: success；scope-empty: rolled_back"
    - entity_type: "CloudAccount"
      min_count: 1
```
