---
journey: "shared-disk-multi-account-metrics"
step: 3
step-action: "Top 榜按 disk_id 去重取代表行"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/shared-disk-multi-account-metrics/journey.md
skip_eval: true
---

# Contract: shared-disk-multi-account-metrics / Step 3: Top 榜按 disk_id 去重取代表行

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "两账号均已完成该共享盘当日采集,ecam_disk_metric 存在该盘多账号行;请求方持有租户有效凭证"
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
- Input: "调用 GET /assets/disk/top?account_id=&days=&sort=&top=&page=&page_size="
- Output: "200 同一 disk_id 在 Top 结果中至多出现一次;代表行取「日期 desc,再使用率 desc」规则选出的行;item 携带去重后账号列表(account_id)、latest(代表行最新一天)、average(按每日代表行计算的均值);不跨账号求和,数值不双计"
- State: "只读,无状态变更"
- Side-effect: "none"

## Outcome "rep-row-tie-break"
- Preconditions: "两账号行日期相同且使用率相同(排序键并列)"
  fixture_spec:
    entities:
      - entity_type: "DiskMetric"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "date"
            value: "同一日期"
          - field: "usage_percent"
            value: "两行使用率相同"
- Input: "调用 Top 榜查询"
- Output: "按既定规则(同日内取唯一代表行)确定唯一代表行,同一 disk_id 仍只出现一次,结果确定且稳定可复现"
- State: "只读,无状态变更"
- Side-effect: "none"

## Outcome "pagination-after-dedup"
- Preconditions: "账号下共享盘较多,请求 top=50(上限)、page/page_size 合法"
  fixture_spec:
    entities:
      - entity_type: "DiskMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "调用 GET /assets/disk/top?top=50&page=1&page_size= 合法分页参数"
- Output: "去重发生在分页之前;每页内 disk_id 不重复;total 为去重后的磁盘总数(分页分母);top 默认 10、最大 50"
- State: "只读,无状态变更"
- Side-effect: "none"

## Outcome "empty-account-scope-empty-items"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_EMPTY_SCOPE(asset_disk_query.go:261-263):resolveAccountScope 结果为空时直接返回空 items 列表与分页元数据,属非错误空态边界 -->
- Preconditions: "查询范围解析后的账号集合为空(如租户无可纳管账号)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "调用 Top 榜查询且账号范围为空"
- Output: "返回空 items 列表与分页元数据(page/page_size),非错误响应"
- State: "只读,无状态变更"
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
          value: "按 Outcome 分支:同盘多账号行或并列行"
```
