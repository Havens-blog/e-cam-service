---
journey: "placeholder-fingerprint-backfill"
step: 1
step-action: "查看占位条目的导入时解析标记"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/journey.md
skip_eval: true
---

# Contract: placeholder-fingerprint-backfill / Step 1: 查看占位条目的导入时解析标记

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "done 快照引用中存在占位指纹引用（扫描时无法解析指纹，如腾讯 SHA-1 口径回退例），引用的 certFingerprint 为按三元组公式重算的确定性占位值；该云支持导入时解析；操作者具备 OpsEngineer 角色"
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
            value: "等于按占位公式派生的确定性占位值"
          - field: "cloud"
            value: "支持导入时解析的云（如 tencent）"
    state_requirements:
      - description: "台账为空"
        prerequisite_entity: "Certificate"
- Input: "运维人员打开发现预览 GET /api/v1/certs/discovery/preview，定位占位指纹条目"
- Output: "该条目在预览中被标记导入时解析（parseable=true 且 parseReason 为 deferred_parse），可勾选——区别于华为云/AWS IAM-hosted 的不可选组"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "real-fingerprint-entry-unmarked"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISCOVERY_PARSE_MARKERS + PLACEHOLDER_FINGERPRINT_FORMULA（discovery_preview_service.go:229-240）：仅当引用指纹恰为公式派生占位值时打 deferred_parse 标记；真实指纹条目 parseable=true 且 reason 为空，构成对照组防止标记误扩散 -->
- Preconditions: "同快照内存在携带真实（非占位公式）指纹的引用条目"
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
            value: "真实证书指纹（非占位公式派生值）"
- Input: "运维人员查看该真实指纹条目的标记"
- Output: "parseable=true 且无导入时解析标记（parseReason 为空）；标记仅精确命中占位公式派生值，不扩散到其它条目"
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
- Output: "401 语义的认证失败响应，不返回预览条目"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 占位指纹保持确定性可重算（公式派生），预览按同公式识别占位条目
- 占位条目与不可选组（华为云/IAM-hosted）口径严格区分

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
    - description: "success 分支引用指纹为公式占位值；real 分支为真实指纹；台账为空"
      prerequisite_entity: "Certificate"
```
