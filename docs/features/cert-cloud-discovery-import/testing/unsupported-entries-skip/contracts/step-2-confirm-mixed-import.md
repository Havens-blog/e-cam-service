---
journey: "unsupported-entries-skip"
step: 2
step-action: "确认导入混合清单"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/journey.md
skip_eval: true
---

# Contract: unsupported-entries-skip / Step 2: 确认导入混合清单

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "清单同时含可解析条目与被强制提交的不可解析条目（华为云/AWS IAM-hosted）；操作者具备 OpsEngineer 角色"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
        field_constraints:
          - field: "cloud"
            value: "至少一条正常云、一条 huawei、一条 aws 非 ARN"
- Input: "以 OpsEngineer 身份 POST /api/v1/certs/discovery/import 确认导入混合清单（含不可解析条目）"
- Output: "202 响应创建导入会话，全部提交条目进入逐条处理队列，会话正常启动"
- State: "会话先持久化再异步执行"
- Side-effect: "异步逐条处理启动"

## Outcome "validation-error-missing-triple"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_VALIDATION_ERRORS（discovery_handler.go:166-171）：条目缺失三元组字段返回 400 INVALID_REQUEST，与条目可解析性无关（请求形态校验先于会话编排） -->
- Preconditions: "请求体某条目缺失三元组字段之一"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "POST /api/v1/certs/discovery/import，某条目缺少 cloudCertId 字段"
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
- 不可解析条目进入会话不阻塞会话创建与其余条目处理

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
      min_count: 3
      relationship_type: "belongs_to"
      parent_entity: "ScanSnapshot"
    - entity_type: "CloudAccount"
      min_count: 1
```
