---
journey: "unsupported-entries-skip"
step: 1
step-action: "查看预览不可选组"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/journey.md
skip_eval: true
---

# Contract: unsupported-entries-skip / Step 1: 查看预览不可选组

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "huawei-group-unselectable"
- Preconditions: "done 快照引用中存在华为云条目；操作者具备 OpsEngineer 角色"
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
          - field: "cloud"
            value: "huawei"
- Input: "运维人员打开发现预览 GET /api/v1/certs/discovery/preview，查看华为云条目组"
- Output: "华为云条目整组标记该云暂不支持自动解析语义（parseable=false，parseReason 为 unsupported_cloud），归入不可选组不可勾选"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "iam-hosted-degraded"
- Preconditions: "快照引用中存在 AWS IAM-hosted 条目（证书 ID 非 ARN 形态）"
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
          - field: "cloud"
            value: "aws"
          - field: "referencedCloudCertId"
            value: "非 arn 前缀形态"
- Input: "运维人员查看 AWS IAM-hosted 条目"
- Output: "该类条目 parseable=false（parseReason 为 iam_hosted），与华为云同语义降级不可选"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "normal-entries-selectable"
<!-- source: inferred -->
<!-- reasoning: Journey Setup 声明快照混有正常可解析条目（阿里/腾讯/Azure/AWS ARN 形态）；Fact Table DISCOVERY_PARSE_MARKERS（discovery_preview_service.go:229-240）：非华为、非 IAM-hosted 条目 parseable=true——对照组验证不可选判定不误伤正常条目 -->
- Preconditions: "同一快照内存在正常可解析条目（阿里/腾讯/Azure/AWS ARN 形态），对应云侧证书仍存在"
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
          - field: "cloud"
            value: "aliyun/tencent/azure/aws(arn 形态) 之一"
- Input: "运维人员查看正常可解析条目"
- Output: "parseable=true 可勾选（parseReason 为空），与不可选组形成对照——不可解析判定统一由可解析标记字段承载，不误伤正常条目"
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
- Output: "401 语义的认证失败响应，不返回预览分组数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 不可选判定统一由可解析标记字段承载（parseable=false 归入不可选组），预览与导入两侧口径一致

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
      field_constraints:
        - field: "cloud"
          value: "覆盖 huawei / aws 非 ARN / 正常云三类条目"
    - entity_type: "CloudAccount"
      min_count: 1
```
