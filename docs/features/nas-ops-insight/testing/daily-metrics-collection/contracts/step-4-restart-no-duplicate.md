---
journey: "daily-metrics-collection"
step: 4
step-action: "服务重启验证当日不重复提交"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/daily-metrics-collection/journey.md
---

# Contract: daily-metrics-collection / Step 4: 服务重启验证当日不重复提交

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "持久化日闸开启且当日已认领成功;采集任务在重启前已提交;同库中 cdn 键同样有当日认领记录可对照"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 2
        field_constraints:
          - field: "resource_type"
            value: "nas 与 cdn 各一条,last_date 均为今日"
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics(重启前已提交)"
- Input: "连续 3 次重启服务进程,每次观察调度器当日提交的 NAS/CDN 采集任务数"
- Output: "每次重启后 NAS/CDN 任务各仅 1 条(当日已认领,重启不再重复提交)"
- State: "scheduler_state 的 nas/cdn last_date 保持为今日;任务队列无新增重复任务"
- Side-effect: "none"

## Outcome "rollback-memory-gate-active"
- Preconditions: "mongo 日闸出现不可恢复故障,运维将特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式切回 false(内存闸回滚模式)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED=false,NAS/CDN 均回滚内存闸"
        prerequisite_entity: "SchedulerState"
- Input: "切换开关后观察 NAS/CDN 调度任务提交"
- Output: "调度任务仍正常提交、指标采集不中断(接受重启重复提交旧缺陷换取调度器可用);回滚原因与重新开启计划被记录"
- State: "内存闸日期字段(而非 scheduler_state)推进为今日;采集链路持续运转"
- Side-effect: "none"

## Outcome "submit-failure-claim-retained"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_SUBMIT_NO_ROLLBACK(auto_sync_nas_metrics.go:79-87):Submit 仅在队列关闭/打满时失败,认领已持久化、提交失败不回滚(回滚会与多副本认领竞争);该边界锁死「认领与提交不原子」是有意设计 -->
- Preconditions: "当日认领已成功持久化,但任务队列提交失败(队列关闭或打满)"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "nas"
          - field: "last_date"
            value: "等于今日"
    state_requirements:
      - description: "任务队列注入 Submit 失败故障"
        prerequisite_entity: "CollectTask"
- Input: "认领成功后提交 nas:collect_metrics 任务失败"
- Output: "ERROR 日志留痕任务标识与日期;当日不再重试提交(不回滚认领)"
- State: "scheduler_state 的 last_date 保持为今日;当日无重试洪泛;次日正常恢复认领提交"
- Side-effect: "none"

## Journey Invariants

- 当日同一资源类型的采集任务至多提交一次;认领持久化在 scheduler_state,重启不重复提交
- 认领后提交失败不回滚认领(避免与多副本竞争),仅 ERROR 留痕
- 回滚开关切换不中断采集链路(降级为旧行为而非停摆)
- cdn 与 nas 日闸按资源类型分键,互相独立
