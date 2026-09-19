---
journey: "view-fs-metric-trend"
step: 1
step-action: "请求单实例趋势"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/view-fs-metric-trend/journey.md
---

# Contract: view-fs-metric-trend / Step 1: 请求单实例趋势

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "目标 fs_id 在 ecam_nas_metric 中已有若干日行(含至少一行正常非零行);运营已登录且持有目标租户资产查看权限"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "capacity"
            value: "至少一行大于 0"
- Input: "GET /assets/nas/metrics?fs_id=<fs>&account_id=<acc>&days=30"
- Output: "200 与响应体 { fs_id, days[] };days 按日期升序;每项含 date/capacity/used/utilization/data_status/qc_status"
- State: "无状态变更(纯读)"
- Side-effect: "none"

## Outcome "days-out-of-range-400"
- Preconditions: "运营已通过鉴权;请求 days=0 或 days=91 等越出 1~90 合法域的值"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "GET /assets/nas/metrics?fs_id=&account_id=&days=0(或 91)"
- Output: "400 参数校验失败,响应体列出校验失败项;days 合法域为 1~90"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "not-found-or-cross-tenant-404"
- Preconditions: "运营已通过鉴权;fs_id 不存在或 account_id 不属于当前租户"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "分属两个租户(或请求的账号不存在)"
- Input: "GET /assets/nas/metrics?fs_id=不存在的fs 或 account_id=租户外账号"
- Output: "404,不泄露资源/账号存在性"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized-401"
- Preconditions: "客户端未携带有效鉴权请求趋势端点"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "无有效凭据的 GET /assets/nas/metrics 请求"
- Output: "401 未认证响应"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "per-account-isolated-series"
- Preconditions: "同一 fs_id 被 2 个及以上账号采集(多活/共享实例),两账号均属当前租户"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "均等于当前会话租户"
      - entity_type: "NASMetric"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fs_id"
            value: "相同 fs_id,两账号各一行"
- Input: "分别以两个 account_id 请求同一 fs_id 的趋势"
- Output: "趋势接口按账号保留各自行,各自返回各自账号视角的日值序列,互不混合"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 趋势响应中 utilization 永远由 capacity/used 现场派生,响应不出现 NaN,capacity=0 时 utilization 为 null
- days[] 永远按日期升序返回;缺失日只以 data_status 标注,绝不用 0/假值填充
- 越权 account_id 一律 404 且不泄露账号存在性;未认证一律 401
- 界面趋势数据仅来自 `ecam_nas_metric` 指标表,不混用资产表 capacity/used_capacity
