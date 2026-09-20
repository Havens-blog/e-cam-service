---
journey: "daily-metrics-collection"
step: 4
step-action: "服务重启验证当日不重复提交"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/daily-metrics-collection/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: daily-metrics-collection / Step 4: 服务重启验证当日不重复提交

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "restart-no-duplicate-submit"
- Preconditions: "当日 oss 日闸已被认领(last_date=今日已持久化);服务进程重启"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "oss"
          - field: "last_date"
            value: "今日"
      - entity_type: "SchedulerStateRecord"
        min_count: 2
        field_constraints:
          - field: "resource_type"
            value: "nas 与 cdn(同日已认领)"
- Input: "连续 3 次重启服务进程,每次重启后观察调度器当日提交的 OSS/NAS/CDN 采集任务数"
- Output: "每次重启后 OSS/NAS/CDN 任务各仅 1 条(当日已认领,重启不再重复提交);无重复采集执行"
- State: "scheduler_state 各资源键 last_date 保持今日不变"
- Side-effect: "none"
- Invariants: "认领持久化在 mongo,重启不丢失当日认领状态"

## Outcome "gate-read-failure-backoff"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1e 定义日闸读失败;Fact Table OSS_GATE_FAILURE_SEMANTICS(daily_gate.go:120-135)读失败设 ≥5 分钟读退避窗口,不逐分钟洪泛;归入本步(调度循环韧性场景)避免 Step 1 超过 5 结果上限 -->
- Preconditions: "故障注入——调度循环读取 oss 日闸状态失败"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
    state_requirements:
      - description: "对 scheduler_state 的读操作持续失败(故障注入)"
        prerequisite_entity: "SchedulerStateRecord"
- Input: "调度循环读取日闸状态"
- Output: "设置至少 5 分钟最短退避窗口后重读;退避窗口内不重复提交任务,不挂在分钟级调度循环上逐分钟洪泛;恢复后正常认领"
- State: "读退避窗口内无任务提交;恢复后按每日一次语义运行"
- Side-effect: "触发日闸读故障升级告警(AlertDailyGateFailure)"

## Outcome "memory-gate-rollback"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1g 定义特性开关切回内存闸回滚验证;Fact Table OSS_MEM_GATE_ROLLBACK(auto_sync_oss_metrics.go:34-45)开关关闭回滚内存闸,采集不中断;归入本步(重启/回滚韧性场景) -->
- Preconditions: "mongo 日闸出现不可恢复故障;运维将 SCHEDULER_PERSISTENT_GATE_ENABLED 切回内存闸"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
    state_requirements:
      - description: "特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭,调度器回滚内存闸模式"
        prerequisite_entity: "SchedulerStateRecord"
- Input: "切换开关后观察 OSS/NAS/CDN 调度任务提交"
- Output: "三类调度任务仍正常提交,指标采集不中断(接受重启重复提交旧缺陷换取调度器可用);回滚原因与重新开启计划被记录"
- State: "调度器运行于内存闸模式;重启后当日可能重复提交(已知并接受的旧行为)"
- Side-effect: "none"

## Journey Invariants

- 回滚开关切换不中断采集链路(降级为旧行为而非停摆),NAS/CDN/OSS 调度同时保持可用
- 当日 oss 采集任务至多提交一次(持久化日闸模式下,无论重启、多副本、手动/自动重叠)
- 日闸读/写失败均走退避+告警,退避窗口内不洪泛任务队列
