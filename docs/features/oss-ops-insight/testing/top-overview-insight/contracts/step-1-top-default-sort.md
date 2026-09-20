---
journey: "top-overview-insight"
step: 1
step-action: "查看账号视角 Top 排名"
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

# Contract: top-overview-insight / Step 1: 查看账号视角 Top 排名

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- density override: Low 风险步携带 3 结果——unauthorized 为 API 表面必需,跨租户 404 为 Journey 定义的安全边界;两者均为纯读廉价校验 -->

## Outcome "top-sorted-by-avg-storage"
- Preconditions: "用户已通过鉴权;租户下多账号合计有多个含指标数据的 OSS bucket(含跨账号同名 bucket);指标数据已落库多日"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "OSSMetricRow"
        min_count: 15
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "多日连续落库"
- Input: "用户请求账号视角 OSS Top 列表(默认 sort=storage_size, top=10)"
- Output: "返回按近 N 天均值口径排序的 Top bucket 列表;bucket_name 去重(同名跨账号 bucket 只出现一次,代表行按日期 desc 再容量 desc 选取);响应含 total/page/page_size 元信息"
- State: "无状态变化(纯读)"
- Side-effect: "none"
- Invariants: "排序口径统一为近 N 天均值;绝不跨账号求和/双计"

## Outcome "cross-tenant-account-404"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1b 定义越权 account_id;Fact Table OSS_CROSS_TENANT_404(service/asset_oss_query.go:45-47)越权映射 404 不泄露账号存在性 -->
- Preconditions: "用户凭证有效,但请求携带不属于本租户的 account_id"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "不属于请求用户所在租户"
- Input: "请求 Top 接口并携带越权 account_id"
- Output: "返回 404(不泄露账号存在性),不返回该账号的任何指标数据"
- State: "无状态变化(请求被租户校验拒绝)"
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

- Top/聚合一律以 ecam_oss_metric 指标表为唯一数据来源;排序口径统一为「近 N 天均值」
- bucket_name 跨账号同名去重取代表行(日期 desc 再容量 desc),绝不跨账号求和/双计
- 租户校验贯穿:越权 account_id 一律 404 且不泄露账号存在性
