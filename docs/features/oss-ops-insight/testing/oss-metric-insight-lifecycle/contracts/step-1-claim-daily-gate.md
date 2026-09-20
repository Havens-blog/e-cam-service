---
journey: "oss-metric-insight-lifecycle"
step: 1
step-action: "调度器认领 oss 日闸并触发当日采集"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/oss-metric-insight-lifecycle/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: oss-metric-insight-lifecycle / Step 1: 调度器认领 oss 日闸并触发当日采集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "claimed-and-task-submitted"
- Preconditions: "持久化日闸可用,scheduler_state 中 oss 键存在且其 last_date 早于今日(当日尚未被认领)"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "oss"
          - field: "last_date"
            value: "早于今日的日期(Asia/Shanghai 自然日)"
    entities_hint: "nas/cdn 日闸键可同时存在,用于验证分键互不干扰"
- Input: "调度器分钟级循环触发 OSS 指标采集检查(当前时间在 00:10 之后)"
- Output: "认领成功(oss 键 last_date 更新为今日),任务队列新增恰好一条 oss:collect_metrics 采集任务,days 参数为 2;日志确认认领成功"
- State: "scheduler_state 的 oss 键 last_date 变为今日;nas/cdn 键的记录与认领行为不受影响"
- Side-effect: "向任务队列提交一条采集任务"
- Invariants: "认领成功是提交采集任务的唯一前提;同一当日至多提交一次"

## Outcome "first-deploy-claim"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1b 定义首部署场景;Fact Table OSS_GATE_CLAIM_CONDITION(auto_sync_oss_metrics.go:22-24)明确首次无 scheduler_state oss 记录视为首次认领 -->
- Preconditions: "首部署环境,scheduler_state 中尚不存在 oss 键记录"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
    state_requirements:
      - description: "scheduler_state 集合可用但无 resource_type=oss 的记录"
        prerequisite_entity: "SchedulerStateRecord"
- Input: "调度器首次触发 OSS 指标采集"
- Output: "视为首次认领并成功,触发一次当日采集任务提交;不回溯补采历史日期"
- State: "scheduler_state 新增 oss 键记录,last_date 为今日;此后按每日一次语义运行"
- Side-effect: "向任务队列提交一条采集任务"

## Outcome "gate-write-failure"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1c 定义故障注入;Fact Table OSS_GATE_FAILURE_SEMANTICS(daily_gate.go:110-191)明确写失败指数退避+告警,auto_sync_oss_metrics.go:53-56 认领失败直接返回不提交任务 -->
- Preconditions: "故障注入——对 scheduler_state 的 oss 键认领写入失败(当日未被认领)"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
    state_requirements:
      - description: "对 scheduler_state 的认领写操作持续失败(故障注入),当日 oss 键未被认领"
        prerequisite_entity: "SchedulerStateRecord"
- Input: "调度器尝试认领 oss 日闸"
- Output: "ERROR 日志记录认领失败;指数退避重试并经 SchedulerGateAlerter 升级告警;不提交采集任务,不静默跳过"
- State: "scheduler_state 的 oss 键保持未认领状态;恢复后当日可正常认领一次"
- Side-effect: "触发日闸故障升级告警(AlertDailyGateFailure)"

## Journey Invariants

- 认领成功是提交采集任务的唯一前提;日闸失败必须走退避重试+告警,绝不静默跳过
- oss 键与 nas/cdn 键按 resource_type 分键,接入 oss 分支不影响既有键行为
- 当日(Asia/Shanghai 自然日)oss 采集任务至多提交一次
