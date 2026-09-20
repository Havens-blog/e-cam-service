---
journey: "top-overview-insight"
step: 4
step-action: "查看运营卡总览"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/top-overview-insight/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: top-overview-insight / Step 4: 查看运营卡总览

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "ops-card-7day-growth"
- Preconditions: "用户已通过鉴权;指标数据已落库多日(可计算近 7 天增速)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSMetricRow"
        min_count: 7
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "覆盖近 7 天窗口"
- Input: "用户打开 OSS 列表页查看顶部运营卡"
- Output: "运营卡数值来自指标表聚合(总容量/对象数/近 7 天增速);增速基于近 7 天窗口计算;数据齐备时正常展示"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "运营卡数值一律来自指标表,资产表快照不参与"

## Outcome "no-data-vs-failure-empty-state"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4b 定义无数据账号运营卡空态;空态区分「无数据」与「采集失败」,采集异常显示警示而非 0 冒充 -->
- Preconditions: "账号下尚无任何 OSS 指标行(未采集或采集失败);用户已通过鉴权"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "该账号在 ecam_oss_metric 无任何指标行"
        prerequisite_entity: "OSSMetricRow"
- Input: "查看该账号视角的运营卡"
- Output: "运营卡空态区分「无数据」与「采集失败」;采集异常时显示警示而非纯空,不显示 0 冒充数据"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Journey Invariants

- 运营卡/聚合一律以 ecam_oss_metric 指标表为唯一数据来源
- 空态区分「无数据」与「采集失败」,不用 0 冒充数据
