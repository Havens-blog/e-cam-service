---
journey: "permission-boundary"
step: 1
step-action: "非 OpsEngineer 访问预览与快照状态端点"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/permission-boundary/journey.md
skip_eval: true
---

# Contract: permission-boundary / Step 1: 非 OpsEngineer 访问预览与快照状态端点

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "已登录用户角色为非 OpsEngineer（如 viewer）；系统存在可用 done 快照与可导入条目（供对照验证数据确实存在但被拒）"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
- Input: "以非 OpsEngineer 角色身份请求 GET /api/v1/certs/discovery/preview 与 GET /api/v1/certs/discovery/snapshot-status"
- Output: "两个端点均返回 403，响应为标准错误信封，不泄漏端点内部细节与堆栈信息"
- State: "无状态变更（权限判定先于任何业务处理与数据访问）"
- Side-effect: "none"

## Outcome "unauthenticated-401"
- Preconditions: "请求不携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证（认证失败先于角色判定）"
        prerequisite_entity: "CloudAccount"
- Input: "匿名请求发现预览/导入/进度端点"
- Output: "返回 401 语义（认证失败先于角色判定），不进入任何业务逻辑"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 全部发现端点恒定同一权限口径：RequireRoles(RoleOpsEngineer)
- 403 响应不泄漏端点内部细节与堆栈信息
- 权限判定先于任何业务处理与数据访问

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "CertReference"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "ScanSnapshot"
    - entity_type: "CloudAccount"
      min_count: 1
```
