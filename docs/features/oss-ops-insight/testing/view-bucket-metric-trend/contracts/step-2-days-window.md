---
journey: "view-bucket-metric-trend"
step: 2
step-action: "指定天数窗口查看"
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

# Contract: view-bucket-metric-trend / Step 2: 指定天数窗口查看

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "custom-window-accepted"
- Preconditions: "用户已通过鉴权;目标 bucket 有多日指标行(覆盖请求窗口)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 30
        field_constraints:
          - field: "date"
            value: "覆盖近 30 天窗口"
- Input: "用户以 days=30 请求趋势窗口"
- Output: "返回近 30 天窗口内的指标序列;days 在 1~90 范围内均被接受;序列按日期升序"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "days-out-of-range-rejected"
<!-- source: inferred -->
<!-- reasoning: Journey Step 2b 定义 days 参数越界;Fact Table OSS_READ_DAYS_BOUND(dao/oss_metric_query.go:15-29)days 限 1~90,越界校验拒绝(400 类) -->
- Preconditions: "用户已通过鉴权;客户端传入 days=0 或 days=91(越出 1~90)"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
- Input: "请求趋势接口携带越界 days"
- Output: "参数校验拒绝(400 类响应),明确提示 days 限 1~90;不越界查询"
- State: "无状态变化(参数校验拒绝)"
- Side-effect: "none"

## Journey Invariants

- `days` 窗口语义固定为 1~90 天,越界一律校验拒绝
