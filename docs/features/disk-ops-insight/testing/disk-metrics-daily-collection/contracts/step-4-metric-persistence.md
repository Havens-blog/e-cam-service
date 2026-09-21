---
journey: "disk-metrics-daily-collection"
step: 4
step-action: "指标落库 ecam_disk_metric"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/journey.md
skip_eval: true
---

# Contract: disk-metrics-daily-collection / Step 4: 指标落库 ecam_disk_metric

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "当日 (account_id, disk_id, date) 唯一键无已存在行,采集返回了带有效日期的指标行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "ecam_disk_metric 中该 (account_id, disk_id, 今日) 行不存在"
        prerequisite_entity: "DiskMetric"
- Input: "采集执行器将今日行经 BulkInsertIfAbsent、昨日及更早行经 BulkUpsertMetrics 写入 ecam_disk_metric"
- Output: "每盘每日 ≥1 行落库;写入成功无错误"
- State: "唯一键 (account_id, disk_id, date) 行建立,含 disk_name/usage_percent/usage_scope/iops/throughput/qc_status/provider 全字段;usage_percent=0 行由写路径门禁强制打 qc_status=zero_exception"
- Side-effect: "Mongo BulkWrite(upsert,ordered=false)"

## Outcome "same-day-idempotent"
- Preconditions: "当日该 (account_id, disk_id, date) 行已存在(调度重跑或手动触发再次写入)"
  fixture_spec:
    entities:
      - entity_type: "DiskMetric"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "今日(YYYY-MM-DD,Asia/Shanghai)"
    state_requirements:
      - description: "同键行今日已有首写结果"
        prerequisite_entity: "DiskMetric"
- Input: "采集执行器再次写入当日指标"
- Output: "今日行保持首写结果不被覆盖($setOnInsert 仅在缺失时插入);昨日及更早行按覆盖更新语义刷新(状态型日快照口径,与 NAS 一致)"
- State: "同键至多一行,今日行字段保持首写值"
- Side-effect: "Mongo BulkWrite 命中已存在行时不修改任何字段"

## Outcome "zero-exception-tagged"
- Preconditions: "磁盘处于未挂载(available)状态或厂商未提供该盘使用率口径,usage_percent 为 0"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "status"
            value: "available(未挂载)"
- Input: "采集执行器采集该盘指标并写库(usage_percent=0 行不做 CDN 式全零过滤)"
- Output: "行原样落库可见;写路径门禁强制打 qc_status=zero_exception,usage_scope 保留口径标注;读取侧可分辨「未挂载/口径缺失」而非当正常空盘"
- State: "ecam_disk_metric 存在带 zero_exception 标注的该盘行"
- Side-effect: "Mongo BulkWrite"

## Outcome "qc-gate-reject"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_QC_GATE(disk_metric.go:87-103):usage_percent 越出 [0,100](含阿里 Burst 系列 -1 哨兵形态)或 iops/throughput 为负时整批拒绝并报错(错误携带 disk_id/date),不良行不得落库 -->
- Preconditions: "适配器返回的任一行 usage_percent 越出 0~100 或 iops/throughput 为负"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集执行器将含越界值的指标批写入 ecam_disk_metric"
- Output: "写入报错(错误携带 disk_id 与 date,提示口径归一自查),整批拒绝;该盘计入失败明细"
- State: "ecam_disk_metric 无任何不良行落库"
- Side-effect: "none"

## Journey Invariants
- 唯一键 (account_id, disk_id, date) 恒成立:任何时刻同一键至多一行,多账号同 disk_id 并存各留一行,互不覆盖
- 同日首写生效:当日已存在的行永不被同日写入覆盖,昨日行仅由次日补采覆盖更新
- 失败隔离:任一厂商/账号的采集失败不阻塞其他厂商/账号,失败必须可观测(计数/日志/告警),不允许静默吞错
- 数据来源唯一:Disk 界面(趋势/Top/运营卡)一律以 ecam_disk_metric 为唯一数据来源,资产表快照数值不得在指标界面展示
- 账号级互斥 + disk 有界并发全程生效,同一账号不会被并发重复采集

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
    - entity_type: "DiskInstance"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
    - entity_type: "DiskMetric"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求同键行不存在;same-day 分支要求今日行已存在;zero-exception 分支要求未挂载盘;qc-gate 分支要求越界指标批"
      prerequisite_entity: "DiskMetric"
```
