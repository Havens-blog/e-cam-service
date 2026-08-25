---
journey: "permission-boundary"
step: 2
step-action: "非 OpsEngineer 访问导入与进度端点"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/permission-boundary/journey.md
skip_eval: true
---

# Contract: permission-boundary / Step 2: 非 OpsEngineer 访问导入与进度端点

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "已登录用户角色为非 OpsEngineer；系统存在可导入条目（数据在场但被拒）"
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
    state_requirements:
      - description: "当前无任何发现导入会话"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "以非 OpsEngineer 角色身份请求 POST /api/v1/certs/discovery/import 与 GET /api/v1/certs/discovery/import/:sessionId"
- Output: "返回 403，不创建任何导入会话，不触发任何云 API 调用"
- State: "零状态副作用（无会话/台账/映射/回填写入）"
- Side-effect: "none"

## Outcome "any-non-ops-role-403"
- Preconditions: "已登录但角色为任意非 OpsEngineer 角色（多角色矩阵枚举：viewer/auditor/ops-supervisor 等全部非 OpsEngineer 角色——发现端点白名单仅含 OpsEngineer）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "登录身份的角色为权限矩阵中任一非 OpsEngineer 角色（含较高权的 supervisor）"
        prerequisite_entity: "CloudAccount"
- Input: "以各非 OpsEngineer 角色分别请求发现导入端点"
- Output: "一律 403（权限矩阵覆盖全部非 OpsEngineer 角色，无遗漏放行）"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 全部发现端点恒定同一权限口径：RequireRoles(RoleOpsEngineer)
- 被拒请求（401/403）不产生任何会话、台账、映射或云 API 副作用

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
  state_requirements:
    - description: "success 分支无既有会话；matrix 分支遍历非 OpsEngineer 角色"
      prerequisite_entity: "DiscoveryImportSession"
```
