---
journey: "disk-day-gate-resilience"
step: 1
step-action: "持久化日闸原子认领"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-day-gate-resilience/journey.md
skip_eval: true
---

# Contract: disk-day-gate-resilience / Step 1: 持久化日闸原子认领

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 开启,持久化日闸机制可用;当日 disk 键尚未被认领"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state 中 disk 键当日未认领(last_date 早于今日)"
        prerequisite_entity: "CloudAccount"
- Input: "每日调度触发,执行器对 scheduler_state disk 键发起当日原子认领"
- Output: "认领结果原子确定:本执行器胜出并提交 disk:collect_metrics 采集任务(days=2)"
- State: "scheduler_state disk 键当日认领持久化落库"
- Side-effect: "采集任务进入任务队列"

## Outcome "already-claimed-skip"
- Preconditions: "当日 disk 键已被认领(本执行器先前认领或多实例中任一方认领)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state disk 键当日 last_date 已等于今日"
        prerequisite_entity: "CloudAccount"
- Input: "每日调度再次触发认领"
- Output: "本执行器跳过,不重复采集,不创建任务"
- State: "当日记录不变"
- Side-effect: "none"

## Outcome "concurrent-single-winner"
- Preconditions: "多个 goroutine/实例同时发起当日 disk 键认领"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "并发认领前 disk 键当日均未被任一方认领"
        prerequisite_entity: "CloudAccount"
- Input: "并发触发多路当日认领"
- Output: "原子认领保证恰好一个胜出,其余全部跳过;不存在双执行或全部跳过(死锁)"
- State: "当日认领归属唯一确定"
- Side-effect: "仅胜出方创建采集任务"

## Outcome "write-failure-backoff-alert"
- Preconditions: "认领时 scheduler_state 写入失败(存储抖动/超时)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "持久化日闸底层存储写入失败"
        prerequisite_entity: "CloudAccount"
- Input: "执行器处理日闸写失败"
- Output: "记录 ERROR 日志并下轮重试;指数退避(基值 1 分钟按失败次数左移,上限 5 分钟)后仍失败则经 SchedulerGateAlerter 升级告警;不静默丢失当日认领,未认领成功时不执行采集"
- State: "当日无认领记录落库(写失败),无采集任务产生"
- Side-effect: "退避重试与告警桥升级告警"

## Journey Invariants
- 原子性:任意并发度下,同一 resource_type 键同一日的认领结果恰好一个胜出
- 持久化一致性:服务重启不改变「当日已认领/已完成」事实,重启 ×3 各仅 1 条
- 故障不静默:写失败/读失败必须走退避与告警路径,禁止热循环与静默吞错
- 既有键零回归:disk 键的接入与回滚不得改变 nas/cdn/oss 键的任何既有行为

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success/already-claimed/concurrent 分支要求 scheduler_state disk 键处于对应认领状态;write-failure 分支要求存储写失败注入"
      prerequisite_entity: "CloudAccount"
```
