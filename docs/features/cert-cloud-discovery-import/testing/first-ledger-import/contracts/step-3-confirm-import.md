---
journey: "first-ledger-import"
step: 3
step-action: "勾选并确认导入"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
skip_eval: true
---

# Contract: first-ledger-import / Step 3: 勾选并确认导入

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "预览已返回未登记可解析条目；操作者具备 OpsEngineer 角色；台账为空"
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
    state_requirements:
      - description: "台账为空，全部预览条目未登记"
        prerequisite_entity: "Certificate"
- Input: "以 OpsEngineer 身份 POST /api/v1/certs/discovery/import，body 为 items 数组，每项含 cloud/accountKey/cloudCertId 三元组（默认勾选全部未登记条目）"
- Output: "202 语义响应：含 sessionId、status=running、items 全部 pending、progress.total 等于提交条数、createdAt；响应为初始快照形态"
- State: "发现导入会话先持久化（running + pending 条目）再启动异步执行；浏览器可安全关闭"
- Side-effect: "异步逐条导入处理在后台开始，处理不随 HTTP 请求生命周期终止"

## Outcome "in-ledger-locked-excluded"
- Preconditions: "条目经双通道判定（台账指纹命中或 CloudCertMapping 命中）inLedger=true"
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
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "与其中一条引用的指纹一致"
- Input: "运维人员在确认导入界面尝试修改已在台账条目的勾选状态并提交导入"
- Output: "inLedger=true 条目灰选不可操作，前端提交的导入请求不包含该类条目；202 会话 items 仅含未登记条目"
- State: "已在台账条目不进入本次导入会话"
- Side-effect: "none"

## Outcome "validation-error-empty-items"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_VALIDATION_ERRORS（discovery_handler.go:161-163）：items 为空数组时 handler 显式返回 400 INVALID_REQUEST "items is required" -->
- Preconditions: "请求体合法 JSON 但 items 为空数组"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "POST /api/v1/certs/discovery/import，body 为空 items 数组"
- Output: "400 响应，错误码 INVALID_REQUEST，消息表明 items 必填"
- State: "不创建任何导入会话"
- Side-effect: "none"

## Outcome "validation-error-missing-triple"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_VALIDATION_ERRORS（discovery_handler.go:166-171）：条目缺失 cloud/accountKey/cloudCertId 任一字段返回 400 INVALID_REQUEST -->
- Preconditions: "请求体 items 非空但某条目缺失三元组字段之一（如 accountKey 为空串）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "POST /api/v1/certs/discovery/import，某条目缺少 accountKey 字段"
- Output: "400 响应，错误码 INVALID_REQUEST，消息表明条目需含 cloud、accountKey 与 cloudCertId"
- State: "不创建任何导入会话"
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
- Output: "401 语义的认证失败响应，不创建任何会话，不进入业务逻辑"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 全部发现端点对非 OpsEngineer 角色一律 403，未认证 401 先于角色判定
- 会话先持久化再异步执行（浏览器中断不丢结果）

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
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "fingerprint"
          value: "in-ledger 分支与一条引用一致"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支台账为空；in-ledger 分支存在指纹命中条目；校验错误分支仅请求形态异常"
      prerequisite_entity: "Certificate"
```
