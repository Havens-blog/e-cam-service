---
journey: "disk-metrics-daily-collection"
step: 5
step-action: "采集结果汇总可观测"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/journey.md
skip_eval: true
---

# Contract: disk-metrics-daily-collection / Step 5: 采集结果汇总可观测

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "本轮采集执行完成(全量运行,任务参数未限定单账号/单厂商)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "采集执行器汇总本轮写入条数、账号数、失败明细与健康检查结果"
- Output: "任务 Result 含 metrics_total/accounts/date_range/skipped_accounts/no_metric_support/accounts_without_disk/failed_disks/failures(provider/account_id/error_count/last_error)/health_alerts;progress 达 100;成功/失败分布可查询"
- State: "任务记录更新为完成"
- Side-effect: "健康检查读取 CountMetricsByProviders 统计必达厂商写库行数"

## Outcome "zero-success-alert"
- Preconditions: "某必达厂商(aliyun/huawei/aws)近 3 天零成功写库行,且该厂商实盘存在 ≥1 个 Disk 实例;本次为全量运行"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "aliyun/huawei/aws 之一"
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "provider"
            value: "与账号厂商一致"
    state_requirements:
      - description: "近 3 天(含今日)窗口内该厂商在 ecam_disk_metric 零行"
        prerequisite_entity: "DiskMetric"
- Input: "健康检查观测采集结果(每日采集完成钩子)"
- Output: "触发 AlertDiskZeroSuccess 升级告警(携带 provider/窗口天数/实例数),厂商名入 Result 的 health_alerts;失败明细(provider/account/error_count/last_error)可用于归因"
- State: "告警落库(Source 前缀 scheduler:disk-health:)"
- Side-effect: "经共用告警桥(schedulerGateAlerter)发出升级告警"

## Outcome "partial-run-no-health-judgment"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_HEALTH_CHECK(disk_health_monitor.go:61-64):params 指定 AccountID 或 Provider 时直接返回空告警清单——手动局部运行不判定,防止以偏概全误报 -->
- Preconditions: "手动触发采集且任务参数限定了单账号或单厂商"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "采集执行器执行局部采集并汇总结果"
- Output: "跳过必达厂商零成功健康判定,不产生健康告警;其余汇总字段照常输出"
- State: "任务 Result 不含 health_alerts 告警项"
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
    - description: "zero-success 分支要求必达厂商近 3 天零写库行且实盘有实例;partial-run 分支要求任务参数限定单账号/单厂商"
      prerequisite_entity: "DiskMetric"
```
