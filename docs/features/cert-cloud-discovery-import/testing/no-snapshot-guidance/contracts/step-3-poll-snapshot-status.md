---
journey: "no-snapshot-guidance"
step: 3
step-action: "轮询快照状态直至终态"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/journey.md
skip_eval: true
---

# Contract: no-snapshot-guidance / Step 3: 轮询快照状态直至终态

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "存在最近 running 状态快照（本方触发或在途既有快照，预览曾返回 NO_SNAPSHOT）；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "前端按固定间隔 GET /api/v1/certs/discovery/snapshot-status 轮询最近快照状态"
- Output: "每次轮询 200 返回最近快照的 hasSnapshot=true、status、startedAt 与 partialFailures；running 期间可持续轮询，不因单次长请求阻塞或被网关/浏览器超时打断；扫描已在途时不重复触发新扫描（含他人先触发的场景，引导直接等待既有 running 快照）"
- State: "无状态变更（只读最近快照，不改变扫描编排语义）"
- Side-effect: "none"

## Outcome "failed-terminal-detail"
- Preconditions: "触发的扫描已收敛到 failed 终态（而非 done），partialFailures 非空"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "failed"
          - field: "partialFailures"
            value: "非空"
- Input: "运维人员查看轮询结果"
- Output: "轮询返回 status=failed、failReason 与 partialFailures 明细（云/产品/账号/原因）；不进入预览；可再次发起扫描重试"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "ScanSnapshot"
- Input: "不带有效凭证 GET /api/v1/certs/discovery/snapshot-status"
- Output: "401 语义的认证失败响应，不返回快照状态"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 引导流程绝不依赖单次长请求同步返回扫描结果（轮询承接，避免网关/浏览器超时打断）
- 快照状态查询端点只读最近快照，不改变扫描编排的同步至终态语义

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ScanSnapshot"
      min_count: 1
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支最近快照 running；failed 分支最近快照 failed 且 partialFailures 非空"
      prerequisite_entity: "ScanSnapshot"
```
