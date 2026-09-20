---
journey: "multi-account-shared-bucket"
step: 4
step-action: "Top 按 bucket_name 去重取代表行"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/multi-account-shared-bucket/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: multi-account-shared-bucket / Step 4: Top 按 bucket_name 去重取代表行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "dedup-single-representative-row"
- Preconditions: "用户已通过鉴权;shared-assets 在 account A/B/C 均有指标行,日期与容量各不相同"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "OSSMetricRow"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "bucket_name"
            value: "shared-assets"
          - field: "date"
            value: "各账号不同日期"
- Input: "用户请求账号视角 Top 列表(GET /assets/oss/top)"
- Output: "shared-assets 在 Top 中只出现一次;代表行来自其所属账号的最新/最大容量行(按日期 desc 再容量 desc 选取);不跨账号求和、不双计"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "rep-row-order-stable"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4b 定义代表行选取次序稳定性;Fact Table OSS_TOP_DEDUP——同日内取 storage_size 最大行为等价实现,排序确定性可守护 -->
- Preconditions: "同一 bucket_name 在多个账号有不同日期/容量的行;多次请求 Top"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 4
        field_constraints:
          - field: "bucket_name"
            value: "shared-assets(跨账号多行)"
- Input: "重复多次请求 Top 列表"
- Output: "代表行按「日期 desc 再容量 desc」确定性选取;多次请求结果稳定一致"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "one-account-empty-kept-in-top"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4c 定义一账号有数据另一账号空;代表行取自有数据账号,缺失不抹除 bucket 也不计入空值 -->
- Preconditions: "shared-assets 在 account A 有指标行;account B 的采集为零/失败(仅 A 有数据);用户已通过鉴权"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "OSSMetricRow"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "account_id"
            value: "仅有数据的账号 A"
- Input: "请求 Top 列表与账号视角统计"
- Output: "代表行取自有数据的账号行;B 的缺失不把 bucket 从 Top 中抹除,也不把 B 的空值计入"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "unauthorized"
<!-- surface-required: API 表面要求——认证端点必须派生 unauthorized 结果 -->
- Preconditions: "请求未携带有效凭证"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
- Input: "无凭证请求 Top 接口"
- Output: "401 Unauthorized;响应体含认证错误信息,不泄露敏感数据"
- State: "无状态变化(请求被中间件拦截)"
- Side-effect: "none"

## Journey Invariants

- Top/聚合视图按 bucket_name 去重取代表行(日期 desc 再容量 desc),绝不跨账号求和/双计
- 租户校验贯穿读取侧:越权 account_id 一律 404 且不泄露账号存在性
