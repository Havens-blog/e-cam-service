---
journey: "no-snapshot-guidance"
step: 1
step-action: "请求预览得到无快照引导"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/journey.md
skip_eval: true
---

# Contract: no-snapshot-guidance / Step 1: 请求预览得到无快照引导

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "系统从未产生任何引用扫描快照（全新系统，无任何状态快照）；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "不存在任何 ScanSnapshot（零快照空态，快照状态端点将返回 hasSnapshot=false 引导首次扫描）"
        prerequisite_entity: "ScanSnapshot"
- Input: "运维人员在无快照状态下以 OpsEngineer 身份请求 GET /api/v1/certs/discovery/preview"
- Output: "预览端点返回结构化错误：409 语义、错误码 NO_SNAPSHOT（非 500），前端展示先执行扫描引导而非报错死路；同口径下快照状态端点为 200 hasSnapshot=false 空态（区分首次扫描与等待重扫两类引导）"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "failed-history-still-no-done"
- Preconditions: "系统存在历史 failed 快照但从未有 done 快照"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "failed"
          - field: "partialFailures"
            value: "非空"
- Input: "运维人员请求云端发现预览 GET /api/v1/certs/discovery/preview"
- Output: "仍返回 409 NO_SNAPSHOT 错误码（done 快照不存在）；引导重扫；failed 快照的 partialFailures 明细可经快照状态端点查看"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证请求 GET /api/v1/certs/discovery/preview"
- Output: "401 语义的认证失败响应，不进入引导逻辑"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- NO_SNAPSHOT 必须为结构化错误码（非 500），保证前端可编程识别并进入引导分支
- 快照状态端点与预览端点同权限口径（非 OpsEngineer 403）

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "failed"   # failed-history 分支；success 分支为零快照
  state_requirements:
    - description: "success 分支无任何快照；failed-history 分支存在 failed 快照但无 done"
      prerequisite_entity: "ScanSnapshot"
```
