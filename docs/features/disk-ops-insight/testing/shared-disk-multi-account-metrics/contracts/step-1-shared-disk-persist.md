---
journey: "shared-disk-multi-account-metrics"
step: 1
step-action: "共享盘在多账号下并存落库"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/shared-disk-multi-account-metrics/journey.md
skip_eval: true
---

# Contract: shared-disk-multi-account-metrics / Step 1: 共享盘在多账号下并存落库

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "同一租户下 ≥2 个云账号均已枚举到同一 disk_id 的共享盘,两账号均已完成当日指标采集"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "同一租户"
      - entity_type: "DiskInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "asset_id"
            value: "两账号指向同一 disk_id(共享盘)"
- Input: "采集执行器分别对账号 A 与账号 B 执行该共享盘的指标采集并写库"
- Output: "ecam_disk_metric 同日存在两行:(account_A, disk_id, date) 与 (account_B, disk_id, date),各行独立成立"
- State: "唯一键 (account_id, disk_id, date) 含 account_id 隔离,两行互不覆盖"
- Side-effect: "Mongo BulkWrite(upsert,ordered=false)"

## Outcome "independent-account-failure"
- Preconditions: "账号 A 采集成功、账号 B 采集失败(或两账号行值不同)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "tenant_id"
            value: "同一租户"
      - entity_type: "DiskInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "asset_id"
            value: "两账号指向同一 disk_id(共享盘)"
- Input: "采集执行器分别采集两账号后落库,账号 B 侧厂商调用或写库失败"
- Output: "两行各自独立成立或缺失:账号 A 的成功行不被账号 B 的失败影响;账号 B 失败计入 Result 的 failures(provider/account_id/error_count/last_error)可观测"
- State: "账号 A 行完好;账号 B 无新行或保持既有行"
- Side-effect: "none"

## Outcome "missing-date-row-skip"
<!-- source: inferred -->
<!-- reasoning: Fact Table sync_disk_metrics.go:393-396:date 为空的行无法定位唯一键 (account_id, disk_id, date),执行器跳过该行不入批——共享盘跨账号批量写入的现实脏数据边界 -->
- Preconditions: "适配器返回的某指标行 date 字段为空"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集执行器将含空日期行的指标批分流写库"
- Output: "该空日期行被跳过不入批,不产生无法定位唯一键的脏行;其余有效行正常落库"
- State: "ecam_disk_metric 无该空日期行"
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
      field_constraints:
        - field: "tenant_id"
          value: "同一租户"
    - entity_type: "DiskInstance"
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
      field_constraints:
        - field: "asset_id"
          value: "指向同一 disk_id 的共享盘"
```
