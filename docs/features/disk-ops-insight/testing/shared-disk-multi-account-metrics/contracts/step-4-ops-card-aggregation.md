---
journey: "shared-disk-multi-account-metrics"
step: 4
step-action: "运营卡聚合去重后口径"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/shared-disk-multi-account-metrics/journey.md
skip_eval: true
---

# Contract: shared-disk-multi-account-metrics / Step 4: 运营卡聚合去重后口径

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "租户下存在共享盘与普通盘,当日指标已落库,请求方持有租户有效凭证"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "同一租户"
      - entity_type: "DiskMetric"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "用户查看 Disk 列表页运营卡(磁盘数/平均使用率/IO 繁忙盘数)"
- Output: "聚合统计基于去重后的代表行:共享盘只计一次,不因多账号并存而重复计入盘数、均值或繁忙盘数"
- State: "只读,无状态变更"
- Side-effect: "none"

## Outcome "removed-account-excluded"
- Preconditions: "账号 B 已从租户移除纳管,其历史指标行仍留在 ecam_disk_metric"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "当前租户"
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "已移除账号 B 的历史行保留在库,但 B 不在当前租户可见账号集合中"
        prerequisite_entity: "DiskMetric"
- Input: "查询租户视角的 Top 榜与运营卡"
- Output: "聚合口径基于当前租户可见账号集合;已移除账号的行不进入当前租户聚合,不产生幽灵盘数或虚增容量"
- State: "历史行保留在库但聚合不可见"
- Side-effect: "none"

## Outcome "busy-share-zero-counted"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_AVG_SKIP_RULE(asset_disk_query.go:139-150,420-442):usage_scope=busy_share 的 0 值是合法闲盘真数据,参与均值与聚合且 data_status=ok,仅口径缺失 0(zero_exception 且非 busy_share)被排除——聚合边界的 0 值甄别 -->
- Preconditions: "某盘 usage_percent=0 且 usage_scope=busy_share(AWS 派生口径全闲盘)"
  fixture_spec:
    entities:
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "usage_percent"
            value: 0
          - field: "usage_scope"
            value: "busy_share"
- Input: "查询含该盘的运营卡聚合"
- Output: "该行按正常真数据参与均值与繁忙盘判定,data_status=ok,不被当异常排除,聚合值不受失真"
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
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求共享盘多账号行并存;removed-account 分支要求账号已移除纳管;busy-share 分支要求 usage_scope=busy_share 的 0 值行"
      prerequisite_entity: "DiskMetric"
```
