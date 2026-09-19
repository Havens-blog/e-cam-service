---
journey: "nas-metric-insight-lifecycle"
step: 3
step-action: "运营查看单实例近 30 天趋势"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/nas-metric-insight-lifecycle/journey.md
---

# Contract: nas-metric-insight-lifecycle / Step 3: 运营查看单实例近 30 天趋势

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "运营已登录且持有目标租户资产查看权限;目标 fs_id 在目标账号名下的 ecam_nas_metric 中存在若干日行(含至少一行正常非零行)"
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
            value: "大于 0 且落在 [1MB, 1PB] 数量级"
          - field: "date"
            value: "今日或近日的 YYYY-MM-DD 字符串"
- Input: "GET /assets/nas/metrics,查询参数 fs_id=目标文件系统、account_id=租户内账号、days=30"
- Output: "200 响应体含 fs_id 与 days 数组;days 按日期升序展开,每项含 date/capacity/used/utilization/data_status/qc_status;另含 latest 与 average 两类汇总值;utilization 由 capacity/used 读取时派生"
- State: "无状态变更(纯读);缺失日以 data_status=missing 标注、capacity/used/utilization 为 null 不填充假值"
- Side-effect: "none"

## Outcome "cross-tenant-account-404"
- Preconditions: "运营持有租户 A 的有效鉴权上下文,但请求携带租户 B 的 account_id(不属于租户 A 账号集合)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "分属两个不同租户"
      - entity_type: "NASMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "GET /assets/nas/metrics?fs_id=&account_id=租户B账号&days=30"
- Output: "404 响应,错误码为账号不存在(404002)语义,不泄露该账号是否存在及其指标数据"
- State: "无状态变更;租户 A 查不到租户 B 的任何 NAS 指标"
- Side-effect: "none"

## Outcome "days-out-of-range-400"
- Preconditions: "运营已通过鉴权;请求的 days 参数越出 1~90 合法域(如 days=0 或 days=91)或为非法整数"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "等于当前会话租户"
- Input: "GET /assets/nas/metrics?fs_id=&account_id=&days=0(或 91、非整数)"
- Output: "400 响应,错误信息指出 days 必须为 1~90 的整数"
- State: "无状态变更;不触发任何指标查询"
- Side-effect: "none"

## Outcome "unauthorized-401"
- Preconditions: "客户端未携带有效登录会话或鉴权上下文缺失,直接请求该需认证端点"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "无有效凭据的 GET /assets/nas/metrics 请求"
- Output: "401 未认证响应,响应体含认证错误信息,不泄露任何指标数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants

- 趋势响应中 utilization 永远由 capacity/used 现场派生,响应不出现 NaN,capacity=0 时 utilization 为 null
- days[] 永远按日期升序返回;缺失日只以 data_status 标注,绝不用 0/假值填充
- 越权 account_id 一律 404 且不泄露账号存在性;未认证一律 401
- 界面趋势数据仅来自 `ecam_nas_metric` 指标表,不混用资产表 capacity/used_capacity
