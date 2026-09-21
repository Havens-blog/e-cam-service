---
journey: "disk-metrics-daily-collection"
step: 2
step-action: "执行器按活跃账号遍历云硬盘"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/journey.md
skip_eval: true
---

# Contract: disk-metrics-daily-collection / Step 2: 执行器按活跃账号遍历云硬盘

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "存在 ≥1 个已纳管且 ecam_instance 中有 disk 类型实例的活跃云账号;该账号当前无进行中的采集任务持有互斥闸"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "当前租户"
      - entity_type: "DiskInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "asset_type"
            value: "disk"
    state_requirements:
      - description: "活跃账号口径以 ecam_instance 本地资产枚举为准,不依赖账号 EnableAutoSync 开关"
        prerequisite_entity: "CloudAccount"
- Input: "采集执行器枚举活跃账号,对每个账号加账号级互斥闸后进入采集"
- Output: "活跃账号清单与 ecam_instance 枚举一致;账号内 disk 按有界并发(上限 5)派发采集,逐盘携带实例真实 region"
- State: "不修改账号与实例数据;账号互斥闸登记该账号,采集完成后释放"
- Side-effect: "按账号逐个调用厂商 DiskMetricQuerier(消耗厂商监控 API 配额)"

## Outcome "account-without-disk-skip"
- Preconditions: "某已纳管账号在 ecam_instance 中无任何 disk 类型实例"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "tenant_id"
            value: "当前租户"
    state_requirements:
      - description: "该账号在 ecam_instance 中零 disk 实例,属非活跃账号"
        prerequisite_entity: "CloudAccount"
- Input: "采集执行器遍历活跃账号"
- Output: "该账号被跳过:不发起厂商查询、不产生指标行、不计入失败;结果汇总 accounts_without_disk 列出该账号"
- State: "ecam_disk_metric 无该账号任何新行"
- Side-effect: "none"

## Outcome "account-busy-skip"
<!-- source: inferred -->
<!-- reasoning: Fact Table DISK_EXECUTOR_ACCOUNT_MUTEX(sync_disk_metrics.go:172-179):tryAcquireAccount 失败时 Warn 日志并计入 skipped_accounts 跳过,是同一账号并发触发的现实边界(手动+定时重叠) -->
- Preconditions: "同一账号已有进行中的采集任务持有账号级互斥闸"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "该账号已被另一采集任务登记在互斥闸中"
        prerequisite_entity: "CloudAccount"
- Input: "采集执行器尝试对该账号加互斥闸并采集"
- Output: "该账号本轮跳过(Warn 日志),计入 skipped_accounts,不计入失败明细"
- State: "账号与指标数据不变"
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
      field_constraints:
        - field: "tenant_id"
          value: "当前租户"
    - entity_type: "DiskInstance"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
      field_constraints:
        - field: "asset_type"
          value: "disk"
  state_requirements:
    - description: "success 分支要求账号有 disk 实例且互斥闸空闲;without-disk 分支要求零 disk 实例;busy 分支要求互斥闸被占"
      prerequisite_entity: "CloudAccount"
```
