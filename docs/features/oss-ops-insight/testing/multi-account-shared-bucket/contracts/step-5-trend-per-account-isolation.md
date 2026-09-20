---
journey: "multi-account-shared-bucket"
step: 5
step-action: "趋势读取按账号精确隔离"
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

# Contract: multi-account-shared-bucket / Step 5: 趋势读取按账号精确隔离

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "per-account-trend-isolated"
- Preconditions: "用户已通过鉴权;account A 与 account B 各自有 shared-assets 的指标序列(数值不同)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "OSSMetricRow"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "bucket_name"
            value: "shared-assets"
          - field: "storage_size"
            value: "A 与 B 互不相同"
- Input: "用户分别以 account A 与 account B 查询 shared-assets 的单 bucket 趋势"
- Output: "各自返回本账号的指标序列,不出现跨账号串数据;两条趋势曲线独立"
- State: "无状态变化(纯读)"
- Side-effect: "none"

## Outcome "cross-tenant-account-404"
<!-- source: inferred -->
<!-- reasoning: Journey Step 5b 定义越权读取;Fact Table OSS_CROSS_TENANT_404(service/asset_oss_query.go:45-47)越权 account_id 映射 404 不泄露账号存在性 -->
- Preconditions: "租户 A 的用户以 account B 的 account_id 查询 shared-assets 趋势(B 不属于租户 A);用户凭证本身有效"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "不属于请求用户所在租户"
- Input: "请求单 bucket 趋势接口并携带越权 account_id"
- Output: "返回 404(不泄露账号存在性),不返回 B 的任何指标数据"
- State: "无状态变化(请求被租户校验拒绝)"
- Side-effect: "none"

## Outcome "unauthorized"
<!-- surface-required: API 表面要求——认证端点必须派生 unauthorized 结果 -->
- Preconditions: "请求未携带有效凭证"
  fixture_spec:
    entities:
      - entity_type: "OSSMetricRow"
        min_count: 1
- Input: "无凭证请求单 bucket 趋势接口"
- Output: "401 Unauthorized;响应体含认证错误信息,不泄露敏感数据"
- State: "无状态变化(请求被中间件拦截)"
- Side-effect: "none"

## Journey Invariants

- 租户隔离贯穿读取侧:越权 account_id 一律 404 且不泄露账号存在性
- 趋势读取按 account_id 精确隔离,跨账号同名 bucket 序列互不串扰
