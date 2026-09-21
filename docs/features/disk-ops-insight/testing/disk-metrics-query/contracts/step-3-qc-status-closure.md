---
journey: "disk-metrics-query"
step: 3
step-action: "分辨 qc_status 异常行"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-query/journey.md
skip_eval: true
---

# Contract: disk-metrics-query / Step 3: 分辨 qc_status 异常行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "租户 T 下存在 usage_percent=0 且 qc_status=zero_exception(口径缺失)的落库行,请求方持有有效凭证"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "租户 T"
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "usage_percent"
            value: 0
          - field: "qc_status"
            value: "zero_exception"
- Input: "用户查看趋势/Top 响应中 usage_percent=0 的磁盘"
- Output: "写路径的 qc_status=zero_exception 在读取响应中原样透出(qc_status 字段)并映射进 data_status=zero_exception,前端可分辨「使用率 0 是异常」而非当正常空盘"
- State: "只读无副作用"
- Side-effect: "none"

## Outcome "empty-window-honest-null"
- Preconditions: "指定盘在请求 days 区间内无任何指标行(如启用日前或新纳管盘)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "租户 T"
    state_requirements:
      - description: "该盘在请求窗口内零指标行"
        prerequisite_entity: "DiskMetric"
- Input: "调用单盘趋势查询指定该盘"
- Output: "返回空趋势列表或全 data_status 标注的空态语义(区分「无数据」与「采集失败/未启用」),不构造假值;近 N 天均值为空(null)而非 0"
- State: "只读无副作用"
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
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求 zero_exception 行存在;empty-window 分支要求请求窗口内零指标行"
      prerequisite_entity: "DiskMetric"
```
