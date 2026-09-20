---
journey: "oss-metric-insight-lifecycle"
step: 5
step-action: "用户在抽屉「监控」tab 查看双轴趋势"
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

# Contract: oss-metric-insight-lifecycle / Step 5: 用户在抽屉「监控」tab 查看双轴趋势

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "dual-axis-trend-rendered"
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
- Input: "用户点开 bucket 抽屉切换到「监控」tab,请求该 bucket 的天粒度指标序列"
- Output: "返回容量(GB)与对象数两条序列,同时含「最新一天」与「近 N 天均值」两类值;前端渲染容量柱+对象数线双轴趋势图;数值全部来自指标表"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "缺失日由 data_status 标注,不填假值"

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
- 趋势同时呈现「最新一天」与「近 N 天均值」两类值;缺失日只标注不填假值
