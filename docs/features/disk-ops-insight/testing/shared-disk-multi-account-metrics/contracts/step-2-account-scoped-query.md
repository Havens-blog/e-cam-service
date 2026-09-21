---
journey: "shared-disk-multi-account-metrics"
step: 2
step-action: "账号视角查询看到本账号行"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/shared-disk-multi-account-metrics/journey.md
skip_eval: true
---

# Contract: shared-disk-multi-account-metrics / Step 2: 账号视角查询看到本账号行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "请求方持有当前租户有效凭证;account_id=A 属于当前租户账号集合;账号 A 与账号 B 均有该共享盘的当日行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "同一租户"
      - entity_type: "DiskMetric"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "disk_id"
            value: "同一共享盘 ID"
- Input: "以账号 A 视角调用 GET /assets/disk/metrics?disk_id=<共享盘>&account_id=A&days="
- Output: "200 仅返回账号 A 的行;账号 B 的同行数据不出现;响应含最新一天值与近 N 天均值两类值"
- State: "只读,无状态变更"
- Side-effect: "none"

## Outcome "cross-account-404"
- Preconditions: "客户端传入 account_id=B 但该账号不属于当前租户账号集合(或以租户外身份查询)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "不属于请求方租户"
- Input: "调用 GET /assets/disk/metrics 指定越权 account_id=B"
- Output: "404 响应,不泄露账号存在性;响应中无该账号任何数据"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效鉴权凭证(token 缺失或过期)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证调用 GET /assets/disk/metrics"
- Output: "401 语义的认证失败响应,不进入业务逻辑,不泄露敏感数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 唯一键 (account_id, disk_id, date) 恒成立:多账号同 disk_id 并存各留一行,任何写入不得跨账号覆盖
- Top 查询对同一 disk_id 至多返回一行(代表行:日期 desc 再使用率 desc),任何读取路径不跨账号求和/双计
- 租户隔离恒成立:account_id 校验失败一律 404 且不泄露账号存在性
- 去重先于分页与聚合:分页结果与运营卡统计均基于去重后的代表行

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 2
    - entity_type: "DiskMetric"
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
      field_constraints:
        - field: "disk_id"
          value: "同一共享盘 ID,每账号各一行"
  state_requirements:
    - description: "success 分支要求 account_id 属于租户;cross-account 分支要求 account_id 越权;unauthorized 分支仅要求未认证"
      prerequisite_entity: "DiskMetric"
```
