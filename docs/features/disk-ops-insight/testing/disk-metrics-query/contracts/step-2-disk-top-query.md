---
journey: "disk-metrics-query"
step: 2
step-action: "查询账号视角 Top 榜"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-query/journey.md
skip_eval: true
---

# Contract: disk-metrics-query / Step 2: 查询账号视角 Top 榜

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "用户持有租户 T 有效凭证;租户内账号已有指标行"
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
- Input: "调用 GET /assets/disk/top?account_id=<账号>&days=7&sort=usage_percent&top=10&page=1&page_size=20"
- Output: "200 返回按近 N 天均值口径排序的磁盘 Top 列表,含分页元数据(total/page/page_size);sort 取值限定 usage_percent|iops|throughput(缺省 usage_percent);top 默认 10 最大 50"
- State: "只读无副作用,重复调用幂等"
- Side-effect: "none"

## Outcome "validation-error"
- Preconditions: "请求参数越界(如 days=0、days=91、sort=latency、top=100)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "调用趋势或 Top 接口携带非法参数(days 越出 1~90、sort 非枚举值、top 超上限 50)"
- Output: "400 Bad Request,响应体明确列出校验失败项(days 限 1~90,sort 仅支持 usage_percent|iops|throughput,top 上限 50;disk_id/account_id 缺失同样 400)"
- State: "无状态变更,不进入业务查询"
- Side-effect: "none"

## Outcome "cross-tenant-404"
- Preconditions: "客户端传入的 account_id 不属于当前租户的账号集合"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "不属于请求方租户"
- Input: "调用趋势或 Top 接口指定该越权 account_id"
- Output: "404(不泄露账号存在性);响应中无该账号任何数据"
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
    - entity_type: "DiskMetric"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求指标行存在;validation-error 分支仅要求非法请求形态;cross-tenant 分支要求越权 account_id"
      prerequisite_entity: "DiskMetric"
```
