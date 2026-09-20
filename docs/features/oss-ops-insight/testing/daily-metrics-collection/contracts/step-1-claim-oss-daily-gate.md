---
journey: "daily-metrics-collection"
step: 1
step-action: "oss 日闸原子认领当日"
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

# Contract: daily-metrics-collection / Step 1: oss 日闸原子认领当日

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "claimed-once-and-submitted"
- Preconditions: "持久化日闸可用,scheduler_state 中 oss 键 last_date 早于今日;nas/cdn 日闸键已存在既有记录"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "oss"
          - field: "last_date"
            value: "早于今日"
      - entity_type: "SchedulerStateRecord"
        min_count: 2
        field_constraints:
          - field: "resource_type"
            value: "nas 或 cdn(既有记录)"
- Input: "调度器触发 OSS 指标采集,对 scheduler_state 执行一次条件认领(resource_type=oss AND last_date<today,更新为 today)"
- Output: "认领成功返回当日认领权;恰好提交一条 oss:collect_metrics 采集任务;oss/nas/cdn 按资源类型分键互不干扰"
- State: "oss 键 last_date 变为今日;nas/cdn 键记录不变"
- Side-effect: "向任务队列提交一条采集任务"
- Invariants: "认领成功是提交采集任务的唯一前提"

## Outcome "multi-replica-race-single-winner"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1b 定义多副本竞争;Fact Table OSS_GATE_CLAIM_CONDITION(auto_sync_oss_metrics.go:52-60)明确 findOneAndUpdate 原子认领,未认领方静默跳过不提交 -->
- Preconditions: "多副本部署,当日 oss 键尚未被认领;两个实例同日同时触发认领"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
    state_requirements:
      - description: "两个调度器实例并发对同一 scheduler_state oss 键执行条件认领"
        prerequisite_entity: "SchedulerStateRecord"
- Input: "两个实例并发执行 oss 日闸认领"
- Output: "仅一个实例认领成功并提交采集任务;另一个实例条件不匹配认领失败,静默跳过不提交、不报错"
- State: "oss 键 last_date 被唯一胜者更新为今日;任务队列当日仅一条 oss 采集任务"
- Side-effect: "仅胜者实例提交一条采集任务"

## Outcome "manual-auto-overlap-single-claim"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1c 定义手动触发与自动调度重叠;同一日闸键保证同一资源当日只被一个任务认领 -->
- Preconditions: "当日自动调度已认领 oss 日闸(采集任务已提交);运营当日又手动执行 oss:collect_metrics"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "oss"
          - field: "last_date"
            value: "今日"
- Input: "手动触发的采集任务尝试经日闸认领当日"
- Output: "手动任务认领失败(当日已认领),不产生重复采集任务;当日仍只有自动任务在执行"
- State: "oss 键 last_date 保持今日不变"
- Side-effect: "none"

## Outcome "gate-write-failure-backoff-alert"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1d 定义日闸写失败;Fact Table OSS_GATE_FAILURE_SEMANTICS(daily_gate.go:110-191)写失败指数退避+AlertDailyGateFailure,退避期间不洪泛队列 -->
- Preconditions: "故障注入——对 scheduler_state 的 oss 键认领写入连续失败(模拟 3 次)"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
    state_requirements:
      - description: "认领写操作持续失败(故障注入),当日 oss 键未被认领"
        prerequisite_entity: "SchedulerStateRecord"
- Input: "调度循环反复尝试认领 oss 日闸"
- Output: "指数退避重试并升级告警触发(AlertDailyGateFailure);退避期间任务队列不被洪泛;恢复后当日正常认领一次并提交任务"
- State: "退避窗口内 oss 键保持未认领;恢复后 last_date 更新为今日"
- Side-effect: "触发日闸故障升级告警"

## Outcome "first-deploy-no-record"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1f 定义首部署无 oss 日闸记录;Fact Table OSS_GATE_CLAIM_CONDITION 明确首次无记录视为首次认领,不回溯补采历史 -->
- Preconditions: "首部署环境,scheduler_state 尚无 oss 记录(last_date 为空)"
  fixture_spec:
    entities:
      - entity_type: "SchedulerStateRecord"
        min_count: 1
- Input: "调度器首次触发 OSS 指标采集"
- Output: "视为首次认领,认领后触发一次当日提交;不回溯补采历史日期"
- State: "scheduler_state 新增 oss 键记录,last_date 为今日"
- Side-effect: "向任务队列提交一条采集任务"

## Journey Invariants

- 当日(Asia/Shanghai 自然日)oss 资源类型的采集任务至多提交一次,无论重启、多副本、手动/自动重叠
- 认领成功是提交采集任务的唯一前提;日闸写失败必须走退避重试+告警,绝不静默跳过
- oss 键与 nas/cdn 键按 resource_type 分键,接入 oss 分支不影响既有键行为
