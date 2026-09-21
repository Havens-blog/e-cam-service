---
journey: "disk-metrics-query"
step: 1
step-action: "查询单盘趋势"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-query/journey.md
skip_eval: true
---

# Contract: disk-metrics-query / Step 1: 查询单盘趋势

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "用户持有租户 T 的有效鉴权凭证;指定 account_id 属于租户 T 账号集合;ecam_disk_metric 已有该盘若干日指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "租户 T"
      - entity_type: "DiskMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "disk_id"
            value: "同一被查询盘的 ID"
- Input: "调用 GET /assets/disk/metrics?disk_id=<盘ID>&account_id=<账号>&days=30(days 缺省 30,限 1~90)"
- Output: "200 返回该盘 30 天内逐日趋势(days 数组按日期升序),同时包含「最新一天」值与「近 N 天均值」两类值;缺失日以 data_status=missing 标注,不填假值"
- State: "只读无副作用,重复调用幂等"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效鉴权凭证(token 缺失或过期)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "无凭证调用任一 Disk 指标读取接口"
- Output: "401 Unauthorized,响应体含认证错误信息,不泄露敏感数据,不进入业务逻辑"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 租户隔离恒成立:所有读取接口从鉴权上下文取 tenantID,account_id 校验失败一律 404 且不泄露账号存在性
- 参数边界恒成立:days ∈ [1,90],sort ∈ {usage_percent, iops, throughput},top 默认 10 最大 50
- 诚实数据:缺失日以 data_status 标注,任何路径不填假值、不把异常 0 当正常值
- 只读无副作用:读取接口不产生状态变更,重复调用幂等

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
      field_constraints:
        - field: "tenant_id"
          value: "租户 T"
    - entity_type: "DiskMetric"
      min_count: 3
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求多日指标行存在;unauthorized 分支仅要求未认证请求"
      prerequisite_entity: "DiskMetric"
```
