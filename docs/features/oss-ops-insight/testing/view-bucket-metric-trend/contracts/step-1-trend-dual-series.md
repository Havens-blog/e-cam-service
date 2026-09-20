---
journey: "view-bucket-metric-trend"
step: 1
step-action: "查看单 bucket 容量/对象数趋势"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/view-bucket-metric-trend/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: view-bucket-metric-trend / Step 1: 查看单 bucket 容量/对象数趋势

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- density override: Low 风险步携带 4 结果——1b/1c 为 Journey 定义的空态区分核心场景(SC-6),unauthorized 为 API 表面必需;均为纯读廉价校验 -->

## Outcome "dual-series-with-latest-and-average"
- Preconditions: "用户已通过鉴权;目标 bucket 在指标表已有包括今日在内的多日指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
      - entity_type: "OSSMetricRow"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "OSSBucketAsset"
        field_constraints:
          - field: "date"
            value: "连续多日含今日"
- Input: "用户打开 bucket 抽屉「监控」tab,请求该 bucket 的天粒度指标序列"
- Output: "返回容量(GB)与对象数两条序列,同时含「最新一天」与「近 N 天均值」两类值(latest/average);前端渲染容量柱+对象数线双轴趋势图;数值全部来自指标表"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "读取侧一律以指标表为唯一数据来源,资产表快照不出现在响应中"

## Outcome "no-data-empty-state"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1b 定义 bucket 无任何指标数据;空态返回非报错,不构造空趋势假图 -->
- Preconditions: "用户已通过鉴权;新纳管的 bucket 尚无任何采集行"
  fixture_spec:
    entities:
      - entity_type: "OSSBucketAsset"
        min_count: 1
    state_requirements:
      - description: "该 bucket 在 ecam_oss_metric 无任何指标行"
        prerequisite_entity: "OSSBucketAsset"
- Input: "请求该 bucket 趋势"
- Output: "空态返回(非报错),days 序列为逐日 missing 标注;前端展示「无数据」空态;不构造空趋势假图"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "collect-failure-warning-empty-state"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1c 定义采集失败/未启用的空态区分;空态能区分「采集失败/未启用」与「无数据」 -->
- Preconditions: "用户已通过鉴权;bucket 所属厂商采集持续失败或指标采集未启用"
  fixture_spec:
    entities:
      - entity_type: "OSSBucketAsset"
        min_count: 1
    state_requirements:
      - description: "该 bucket 无指标行且采集侧存在失败/未启用状态可辨"
        prerequisite_entity: "OSSBucketAsset"
- Input: "请求该 bucket 趋势并观察空态"
- Output: "空态能区分「采集失败/未启用」与「无数据」;采集异常时展示警示而非纯空"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "unauthorized"
<!-- surface-required: API 表面要求——认证端点必须派生 unauthorized 结果 -->
- Preconditions: "请求未携带有效凭证(或凭证过期)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
- Input: "无凭证请求单 bucket 趋势接口"
- Output: "401 Unauthorized;响应体含认证错误信息,不泄露敏感数据"
- State: "无状态变化(请求被中间件拦截)"
- Side-effect: "none"

## Journey Invariants

- 读取侧一律以 ecam_oss_metric 指标表为唯一数据来源,资产表快照数值不出现在趋势响应中
- 空态区分「无数据」与「采集失败/未启用」,不构造假趋势图
