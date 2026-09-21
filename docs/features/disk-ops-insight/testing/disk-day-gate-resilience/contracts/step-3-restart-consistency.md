---
journey: "disk-day-gate-resilience"
step: 3
step-action: "服务重启后状态一致"
generated: "2026-09-21"
sources:
  - docs/features/disk-ops-insight/testing/disk-day-gate-resilience/journey.md
skip_eval: true
---

# Contract: disk-day-gate-resilience / Step 3: 服务重启后状态一致

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "当日已认领/已完成日闸后服务重启(模拟 ×3)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "DiskMetric"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "scheduler_state disk 键当日已认领,重启不改变持久化事实"
        prerequisite_entity: "CloudAccount"
- Input: "每次重启后观察调度行为"
- Output: "每次重启后当日采集各仅触发 1 条(共 1 次成功执行);持久化状态防止重复采集,也不丢当日认领"
- State: "scheduler_state 状态跨重启一致"
- Side-effect: "none"

## Outcome "feature-flag-rollback"
- Preconditions: "运维关闭特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED(回滚场景)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "持久化日闸特性开关显式关闭"
        prerequisite_entity: "CloudAccount"
- Input: "观察回滚后的调度行为"
- Output: "调度切换回内存闸兜底,NAS/CDN/OSS/Disk 四资源调度仍可用;不因回滚产生重复采集或调度停摆"
- State: "调度模式从持久化闸切换到内存闸,采集不中断"
- Side-effect: "none"

## Outcome "no-gate-assembly-safe-skip"
<!-- source: inferred -->
<!-- reasoning: Fact Table auto_sync_disk_metrics.go:42-47:dailyGate 为 nil(未接 mongo 的最小装配)且开关开启时安全跳过,显式不降级回内存闸——内存闸的重启重复提交缺陷正是本闸要修的问题 -->
- Preconditions: "特性开关开启但服务未装配持久化日闸(如未接 mongo 的最小装配)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "每日调度触发 disk 采集检查"
- Output: "本轮安全跳过(静默,无任务创建),不降级回内存闸,不产生重启重复提交旧缺陷"
- State: "无状态变更"
- Side-effect: "none"

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
    - entity_type: "DiskMetric"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "CloudAccount"
  state_requirements:
    - description: "success 分支要求当日已认领后重启;rollback 分支要求特性开关关闭;safe-skip 分支要求日闸未装配"
      prerequisite_entity: "CloudAccount"
```
