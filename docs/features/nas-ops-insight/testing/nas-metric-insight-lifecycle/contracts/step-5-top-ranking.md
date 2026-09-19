---
journey: "nas-metric-insight-lifecycle"
step: 5
step-action: "运营查看 Top 排行识别高水位实例"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/nas-metric-insight-lifecycle/journey.md
---

# Contract: nas-metric-insight-lifecycle / Step 5: 运营查看 Top 排行识别高水位实例

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已登录且持有租户资产查看权限;租户账号近 N 天存在指标行,且存在多账号共享 fs 以验证去重"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "近 N 天内的 YYYY-MM-DD 字符串"
- Input: "GET /assets/nas/top?account_id=&days=&sort=utilization&top=10&page=1&page_size=10"
- Output: "200 响应含 total/page/page_size/items;items 按 sort 字段近 N 天均值口径降序取前 N;每条含 fs_id/fs_name/account_id 列表/data_status/qc_status 与最新一天、近 N 天均值两类 capacity·used·utilization;共享 fs 不双计"
- State: "无状态变更(纯读)"
- Side-effect: "none"

## Outcome "unauthorized-401"
- Preconditions: "客户端未携带有效登录会话请求 Top 端点"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "无有效凭据的 GET /assets/nas/top 请求"
- Output: "401 未认证响应,不泄露任何排行数据"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "sort-invalid-400"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_SORT_DOMAIN(asset_handler_nas_metrics.go:79-83):sort 仅接受 capacity|utilization,非法值 400;journey Step 5 使用合法值 sort=utilization,该边界为同端点必须覆盖的参数校验面 -->
- Preconditions: "运营已通过鉴权;请求携带 sort 参数为合法域 capacity|utilization 之外的值"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
- Input: "GET /assets/nas/top?sort=非法排序值"
- Output: "400 响应,错误信息指出 sort 仅支持 capacity 或 utilization"
- State: "无状态变更;不触发任何聚合查询"
- Side-effect: "none"

## Journey Invariants

- 任何聚合视图必须先按 fs_id 去重再计数,共享 fs 永不双计
- Top 的 sort 仅接受 capacity|utilization,utilization 一律取近 N 天均值口径
- 聚合口径以最新日期行的厂商返回值为准,绝不跨账号容量求和或平均
- 越权 account_id 一律 404 且不泄露账号存在性;未认证一律 401
