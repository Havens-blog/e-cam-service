---
journey: "oss-metric-insight-lifecycle"
step: 6
step-action: "验证采集异常时的警示空态"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/oss-metric-insight-lifecycle/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: oss-metric-insight-lifecycle / Step 6: 验证采集异常时的警示空态

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "failure-warning-empty-state"
- Preconditions: "用户已通过鉴权;目标 bucket 所属厂商采集持续失败或指标采集未启用,指标表无该 bucket 数据"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "该 bucket 在 ecam_oss_metric 无任何指标行,且采集侧存在失败/未启用状态可辨"
        prerequisite_entity: "OSSBucketAsset"
- Input: "用户查看该 bucket 的运营卡与抽屉趋势空态"
- Output: "空态区分「无数据」与「采集失败/未启用」;采集异常时运营卡显示警示而非纯空,不显示 0 冒充数据"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "空态绝不构造假趋势图或用 0 冒充数据"

## Outcome "no-data-empty-state"
<!-- source: inferred -->
<!-- reasoning: 与采集失败空态互斥的对照场景——bucket 正常但尚未到采集时间(如新纳管);Journey Step 6 预期结果要求两类空态可区分,Fact Table OSS_DATA_STATUS_VALUES 支持 missing 语义 -->
- Preconditions: "用户已通过鉴权;bucket 正常纳管但尚无任何指标行(从未采集,无失败状态)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "ecam_oss_metric 中无该 bucket 任何指标行,且无采集失败记录"
        prerequisite_entity: "OSSBucketAsset"
- Input: "用户查看该 bucket 的运营卡与抽屉趋势空态"
- Output: "展示「无数据」空态(区别于采集异常警示);不报错、不构造空趋势假图"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Journey Invariants

- 空态区分「无数据」与「采集失败/未启用」两种语义,采集异常时显示警示而非纯空
- OSS 界面一律以指标表为唯一数据来源;空态不构造假数据
