---
journey: "unsupported-entries-skip"
step: 4
step-action: "查看终态与逐条原因"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/journey.md
skip_eval: true
---

# Contract: unsupported-entries-skip / Step 4: 查看终态与逐条原因

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "混合清单导入会话已收敛终态：可解析条目全部成功，不可解析条目按跳过语义计入失败侧"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_failed"
          - field: "items"
            value: "成功条目与记因跳过条目并存"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "hostingStatus"
            value: "fingerprint_only"
- Input: "运维人员以 OpsEngineer 身份 GET /api/v1/certs/discovery/import/:sessionId 查看会话终态与逐条原因"
- Output: "会话收敛 completed/partial_failed（跳过条目按既有语义计入失败侧但不中断会话）；可解析条目全部正常登记；progress 计数与条目结果一致"
- State: "无状态变更（验证读）"
- Side-effect: "none"

## Outcome "all-unparseable-terminal"
- Preconditions: "勾选提交的条目全部为不可解析类（华为云/IAM-hosted/云侧已删除）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_failed"
          - field: "items"
            value: "全部条目记因失败"
    state_requirements:
      - description: "不产生任何台账/映射/引用回填写入"
        prerequisite_entity: "Certificate"
- Input: "运维人员等待会话终态并查看"
- Output: "会话收敛终态（全部条目逐条记因可见），不产生任何台账/映射/引用回填写入，无静默丢失"
- State: "零业务写入"
- Side-effect: "none"

## Outcome "no-leak-static-reasons"
- Preconditions: "某条目失败于云 API 返回异常内容（响应片段或凭证信息可能被携带的形态）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "items"
            value: "含云 API 异常失败条目"
    state_requirements:
      - description: "云 API 错误携带响应片段/凭证等敏感内容"
        prerequisite_entity: "CloudAccount"
- Input: "运维人员查看该条目失败原因"
- Output: "失败原因恒为错误码+静态文案，不携带云响应片段、凭证或内部错误细节（云侧错误细节只进日志不进响应）"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "DiscoveryImportSession"
- Input: "不带有效凭证 GET /api/v1/certs/discovery/import/:sessionId"
- Output: "401 语义的认证失败响应，不返回会话与失败原因数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- errorReason 恒为静态文案，不泄漏云响应内容与凭证
- 跳过条目计入失败侧但不中断会话，终态可判

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "DiscoveryImportSession"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "partial_failed"
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "hostingStatus"
          value: "fingerprint_only"
  state_requirements:
    - description: "all-unparseable 分支无任何证书写入；no-leak 分支云 API 错误携带敏感内容"
      prerequisite_entity: "Certificate"
```
