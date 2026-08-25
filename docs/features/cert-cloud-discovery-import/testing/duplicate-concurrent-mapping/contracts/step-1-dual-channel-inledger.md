---
journey: "duplicate-concurrent-mapping"
step: 1
step-action: "查看预览的双通道已在台账判定"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/journey.md
skip_eval: true
---

# Contract: duplicate-concurrent-mapping / Step 1: 查看预览的双通道已在台账判定

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "条目引用指纹不在台账，且本云本账号不存在 CloudCertMapping（双通道均未命中）；操作者具备 OpsEngineer 角色"
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
      - description: "无与引用指纹一致的 Certificate；无该三元组的 CloudCertMapping"
        prerequisite_entity: "Certificate"
- Input: "运维人员打开发现预览 GET /api/v1/certs/discovery/preview，查看各条目 inLedger 标记"
- Output: "该条目 inLedger=false 可勾选，notAfter 为占位文案"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "fingerprint-channel-hit"
- Preconditions: "台账已有该指纹证书（如手工导入过），但本云本账号 CloudCertMapping 不存在（仅指纹通道命中）"
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
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "与该条目引用指纹一致"
    state_requirements:
      - description: "不存在该三元组的 CloudCertMapping"
        prerequisite_entity: "CloudCertMapping"
- Input: "运维人员查看该条目 inLedger 并（经强制路径）提交导入"
- Output: "预览 inLedger=true 灰选（指纹通道命中），notAfter 显示台账值；若导入执行则走撞指纹补建映射路径记 success，台账不重复"
- State: "无状态变更（预览只读）"
- Side-effect: "none"

## Outcome "mapping-channel-hit"
- Preconditions: "台账证书曾被云端导入过，该三元组的 CloudCertMapping 已命中（如台账记录后续被修改导致指纹比对不中，仅映射通道命中）"
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
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "cloud"
            value: "与条目一致"
          - field: "accountKey"
            value: "与条目一致"
          - field: "cloudCertId"
            value: "与条目一致"
    state_requirements:
      - description: "条目引用指纹与台账任何证书指纹都不一致（指纹通道不命中）"
        prerequisite_entity: "Certificate"
- Input: "运维人员查看该条目 inLedger"
- Output: "inLedger=true 灰选不可选（双通道任一命中即视为已在台账）"
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
- Input: "不带有效凭证请求发现预览"
- Output: "401 语义的认证失败响应，不返回 inLedger 判定数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- inLedger 判定恒为双通道（台账指纹 OR 映射反查），任一命中即 true
- 台账按指纹全局唯一（uk_fingerprint），任何路径不得产生第二条同指纹台账记录

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
    - entity_type: "Certificate"
      min_count: 1
    - entity_type: "CloudCertMapping"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "Certificate"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 双通道均 miss；fingerprint 分支仅指纹命中；mapping 分支仅映射命中"
      prerequisite_entity: "Certificate"
```
