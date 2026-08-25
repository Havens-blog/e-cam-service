---
journey: "permission-boundary"
step: 3
step-action: "OpsEngineer 正常访问对照"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/permission-boundary/journey.md
skip_eval: true
---

# Contract: permission-boundary / Step 3: OpsEngineer 正常访问对照

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "OpsEngineer 角色已登录；系统存在可用 done 快照与可导入条目"
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
- Input: "切换为 OpsEngineer 角色身份请求同一组端点：预览/快照状态/导入/进度"
- Output: "预览与快照状态正常返回数据（200）；导入端点正常创建会话（202 语义）；进度端点正常返回会话状态（200）——证明 403 来自角色判定而非端点不可用"
- State: "授权路径下导入会话正常创建（对照被拒路径零副作用）"
- Side-effect: "导入会话异步执行启动"

## Outcome "rejected-zero-state-residue"
- Preconditions: "非 OpsEngineer 角色的导入请求已被 403 拒绝（存在被拒请求历史）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "存在 403 被拒的导入请求记录，但无任何由其创建的业务数据"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "运维人员事后核对会话集合与台账数据（会话进度端点与台账列表）"
- Output: "不存在被拒请求创建的会话、台账记录、映射或引用回填——403 拒绝路径零状态副作用"
- State: "零残留（验证读）"
- Side-effect: "none"

## Journey Invariants
- 全部发现端点恒定同一权限口径：RequireRoles(RoleOpsEngineer)
- 被拒请求（401/403）不产生任何会话、台账、映射或云 API 副作用
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
  state_requirements:
    - description: "success 分支可导入条目在场；residue 分支存在被拒请求历史且无业务数据"
      prerequisite_entity: "DiscoveryImportSession"
```
