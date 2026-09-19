---
journey: "daily-metrics-collection"
step: 1
step-action: "日闸原子认领当日"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/daily-metrics-collection/journey.md
---

# Contract: daily-metrics-collection / Step 1: 日闸原子认领当日

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "scheduler_state collection 可用,特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 默认开启;nas 键无记录或 last_date 早于今日;至少 1 个活跃账号注册在租户下"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "nas 或无 nas 记录"
          - field: "last_date"
            value: "早于今日或空"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "调度器触发 NAS 采集,对 scheduler_state 执行一次 findOneAndUpdate(条件 resource_type=nas 且 last_date 早于今日,更新为今日)"
- Output: "认领成功返回真;认领成功后恰好提交 1 条 nas:collect_metrics(days=2) 任务"
- State: "scheduler_state 中 nas 键 last_date 更新为今日;任务队列新增一条待处理采集任务"
- Side-effect: "对 scheduler_state 的原子 findOneAndUpdate 写入(upsert)"

## Outcome "claim-already-taken"
- Preconditions: "当日 nas 键已被认领——多副本并发竞争另一实例先认领,或手动触发与每日自动任务同日重叠"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "nas"
          - field: "last_date"
            value: "等于今日的 YYYY-MM-DD 字符串"
- Input: "另一实例或手动任务并发执行认领"
- Output: "认领失败(条件不匹配),不提交任何新任务,静默跳过无告警"
- State: "scheduler_state 的 last_date 保持为今日;全天该资源恰好一条采集任务"
- Side-effect: "none"

## Outcome "gate-write-failure-retry-alert"
- Preconditions: "故障注入——模拟写日闸失败(单轮内连续失败)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state 写路径注入连续 3 次写失败"
        prerequisite_entity: "SchedulerState"
- Input: "认领时写 scheduler_state 失败"
- Output: "按 1 秒/2 秒/4 秒指数退避重试;重试耗尽触发日闸写失败升级告警并进入跨轮 1 分钟起、5 分钟封顶的指数退避;退避期间不洪泛任务队列;恢复后当日正常认领一次"
- State: "last_date 在成功前未被推进;恢复后正常推进并提交一次任务"
- Side-effect: "重试耗尽时调用 AlertDailyGateFailure(resource_type=nas, operation=write) 告警通道"

## Outcome "gate-read-failure-backoff"
- Preconditions: "故障注入——模拟读日闸状态失败(如 mongo 查询错误)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state 读路径注入故障"
        prerequisite_entity: "SchedulerState"
- Input: "调度循环读取日闸状态失败"
- Output: "设置至少 5 分钟的读失败退避窗口后重读;退避窗口内不重复提交任务,不逐分钟洪泛"
- State: "任务队列在退避窗口内无新增该资源任务;读恢复后按正常语义认领"
- Side-effect: "读失败上报告警通道(operation=read)"

## Outcome "first-deploy-initial-claim"
- Preconditions: "首部署时 scheduler_state 尚无 nas 记录,last_date 为空(读接口返回空串的首次认领过渡态)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "ecam_scheduler_state 集合中不存在 resource_type=nas 的文档"
        prerequisite_entity: "SchedulerState"
- Input: "调度器首次触发当日采集"
- Output: "视为首次认领成功,认领后触发一次当日提交;不回溯补采历史"
- State: "scheduler_state 插入 resource_type=nas、last_date=今日 的新文档;此后按每日一次语义运行"
- Side-effect: "none"

## Journey Invariants

- 当日(Asia/Shanghai 自然日)同一资源类型的采集任务至多提交一次,无论重启、多副本、手动/自动重叠
- 认领成功是提交采集任务的唯一前提;日闸写失败必须走退避重试+告警,绝不静默跳过
- cdn 与 nas 日闸按资源类型分键,互相独立认领、互不阻塞
- 首次无记录视为首次认领,认领后当日提交一次,不回溯补采历史
