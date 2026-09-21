---
journey: "disk-metrics-daily-collection"
step: 1
step-action: "调度器认领当日 disk 日闸"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-metrics-daily-collection/journey.md
skip_eval: true
---

# Contract: disk-metrics-daily-collection / Step 1: 调度器认领当日 disk 日闸

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "持久化日闸机制可用(特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 开启),当日 disk 键尚未被任何执行器认领"
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
    state_requirements:
      - description: "scheduler_state 中 disk 键当日未被认领,持久化日闸可用"
        prerequisite_entity: "CloudAccount"
- Input: "每日调度触发,执行器对 scheduler_state 的 disk 键发起当日原子认领(条件:resource_type=disk 且 last_date 早于今日)"
- Output: "恰好一个执行器认领胜出并提交 disk:collect_metrics 采集任务(days=2,补昨日完整行+今日初态);其余并发认领者静默跳过"
- State: "scheduler_state disk 键当日 last_date 更新为今日,持久化落库"
- Side-effect: "采集任务进入异步任务队列,由队列调度执行"

## Outcome "first-claim-transition"
- Preconditions: "scheduler_state 中尚无 disk 键记录(特性启用后的第一天)"
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
    state_requirements:
      - description: "scheduler_state 无 disk 键任何记录,视为首次认领"
        prerequisite_entity: "CloudAccount"
- Input: "执行器发起当日认领"
- Output: "视为首次认领并认领成功,触发一次当日提交;不回溯补采启用日之前的日期(启用日之前无指标行属预期)"
- State: "scheduler_state 新建 disk 键记录并标记当日已认领"
- Side-effect: "创建一次当日采集任务"

## Outcome "already-claimed-skip"
- Preconditions: "当日 disk 键已被认领(本执行器先前认领或多实例中任一方认领)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state disk 键当日 last_date 已等于今日"
        prerequisite_entity: "CloudAccount"
- Input: "每日调度再次触发,执行器尝试当日认领"
- Output: "认领返回未胜出,静默跳过;不创建采集任务,不产生任何指标写入"
- State: "scheduler_state 当日记录保持不变"
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
  state_requirements:
    - description: "success 分支要求 disk 键当日未认领;first-claim 分支要求 disk 键无记录;already-claimed 分支要求当日已认领"
      prerequisite_entity: "CloudAccount"
```
