---
journey: "placeholder-fingerprint-backfill"
step: 2
step-action: "确认导入含占位条目的清单"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/journey.md
skip_eval: true
---

# Contract: placeholder-fingerprint-backfill / Step 2: 确认导入含占位条目的清单

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "预览清单含可勾选的占位指纹条目；对应 cloudCertId 在云侧存在且 GetCert 可返回可净化 PEM；操作者具备 OpsEngineer 角色"
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
        field_constraints:
          - field: "certFingerprint"
            value: "占位公式派生值"
    state_requirements:
      - description: "台账为空；云侧证书存在可返回 PEM"
        prerequisite_entity: "Certificate"
- Input: "以 OpsEngineer 身份 POST /api/v1/certs/discovery/import 勾选含占位指纹条目的清单"
- Output: "202 响应创建导入会话，占位条目进入逐条处理队列"
- State: "会话先持久化再异步执行"
- Side-effect: "异步逐条处理启动"

## Outcome "validation-error-empty-items"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_VALIDATION_ERRORS（discovery_handler.go:161-163）：items 为空数组时返回 400 INVALID_REQUEST -->
- Preconditions: "请求体 items 为空数组"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "POST /api/v1/certs/discovery/import，items 为空数组"
- Output: "400 响应，错误码 INVALID_REQUEST，消息表明 items 必填"
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
- 占位条目导入入口与常规条目一致（同一导入端点与会话语义）

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
      field_constraints:
        - field: "certFingerprint"
          value: "占位公式派生值"
    - entity_type: "CloudAccount"
      min_count: 1
```
