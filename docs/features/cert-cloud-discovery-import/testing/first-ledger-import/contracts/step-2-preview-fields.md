---
journey: "first-ledger-import"
step: 2
step-action: "查看预览清单与快照时点"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
skip_eval: true
---

# Contract: first-ledger-import / Step 2: 查看预览清单与快照时点

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "done 快照内同时存在未登记条目与已在台账条目（台账指纹命中），操作者具备 OpsEngineer 角色"
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
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "与至少一条快照引用的指纹一致"
- Input: "运维人员浏览 GET /api/v1/certs/discovery/preview 的响应条目与快照时点字段"
- Output: "每个条目含七类字段（云/账号/云证书 ID/引用资源数/inLedger 标记/notAfter/可解析标记）；未登记条目 notAfter 显示占位文案（导入后补全）；inLedger=true 条目 notAfter 显示台账值；响应另含 snapshotStartedAt 快照时点"
- State: "无状态变更（消费 Step 1 的同一响应）"
- Side-effect: "none"

## Outcome "stale-snapshot-notice"
- Preconditions: "最近 done 快照的 snapshotStartedAt 距当前时间超过 7 天"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
          - field: "startedAt"
            value: "早于当前时间 7 天以上"
      - entity_type: "CertReference"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
- Input: "运维人员查看预览响应携带的 snapshotStartedAt"
- Output: "响应仍基于该陈旧快照生成清单并如实携带原始 snapshotStartedAt（前端据此显著提示快照超期建议重扫）；预览条目明确标注基于快照时点，不承诺云侧现状"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unparseable-group"
- Preconditions: "预览清单中混有华为云条目与 AWS IAM-hosted（证书 ID 非 ARN 形态）条目，同时存在正常可解析条目"
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
            value: "至少一条 huawei、一条 aws（引用的云证书 ID 非 arn 前缀）、一条其它云"
- Input: "运维人员查看该类条目的可解析标记"
- Output: "华为云条目 parseable=false 归入不可选组（该云暂不支持自动解析语义）；AWS IAM-hosted 条目 parseable=false 同语义降级；两者不可勾选；正常条目 parseable=true 可勾选"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 预览端点始终纯 DB 聚合，绝不调用云 API；未登记条目 notAfter 为占位文案，已在台账条目为台账值
- 预览条目明确基于快照时点（不承诺云侧现状）

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
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "fingerprint"
          value: "与至少一条引用一致（success 分支 inLedger 对照）"
  state_requirements:
    - description: "stale 分支 startedAt 超 7 天；unparseable 分支混有 huawei 与 aws 非 ARN 条目"
      prerequisite_entity: "ScanSnapshot"
```
