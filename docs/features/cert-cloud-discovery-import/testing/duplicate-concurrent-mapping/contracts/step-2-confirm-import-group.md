---
journey: "duplicate-concurrent-mapping"
step: 2
step-action: "确认导入多账号同证书条目组"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/journey.md
skip_eval: true
---

# Contract: duplicate-concurrent-mapping / Step 2: 确认导入多账号同证书条目组

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "预览存在两个不同账号引用同一张证书（内容相同、指纹相同）的条目组，均未登记；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
        field_constraints:
          - field: "accountKey"
            value: "两条分属不同账号，指向同一张云侧证书内容"
    state_requirements:
      - description: "台账为空，两条目 inLedger=false"
        prerequisite_entity: "Certificate"
- Input: "以 OpsEngineer 身份 POST /api/v1/certs/discovery/import 勾选含两个账号同证书条目的条目组"
- Output: "202 响应创建导入会话，两个条目进入逐条处理队列（items 全 pending、progress.total=2）"
- State: "会话先持久化再异步执行"
- Side-effect: "异步逐条处理启动"

## Outcome "validation-error-missing-triple"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_VALIDATION_ERRORS（discovery_handler.go:166-171）：条目缺失三元组字段返回 400 INVALID_REQUEST -->
- Preconditions: "请求体某条目缺失三元组字段之一"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "POST /api/v1/certs/discovery/import，某条目缺少 cloud 字段"
- Output: "400 响应，错误码 INVALID_REQUEST，消息表明条目需含三元组字段"
- State: "不创建会话"
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
- Input: "不带有效凭证 POST /api/v1/certs/discovery/import"
- Output: "401 语义的认证失败响应，不创建会话"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 多账号同证书场景不降级：任一条目撞指纹均走补建映射路径
- 全部发现端点对非 OpsEngineer 角色一律 403

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
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "ScanSnapshot"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支两条引用分属不同账号且指向同一证书内容，台账为空"
      prerequisite_entity: "Certificate"
```
