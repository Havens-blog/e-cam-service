---
journey: "nas-metric-insight-lifecycle"
step: 1
step-action: "触发当日 NAS 指标采集(持久化日闸认领+任务提交)"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/nas-metric-insight-lifecycle/journey.md
---

# Contract: nas-metric-insight-lifecycle / Step 1: 触发当日 NAS 指标采集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "持久化日闸特性开关默认开启且 mongo 可写;scheduler_state 中 nas 键的 last_date 早于今日(Asia/Shanghai 自然日);租户下存在至少 1 个已纳管云账号且该账号在实例表中存在至少 1 个 NAS 文件系统实例"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "nas"
          - field: "last_date"
            value: "早于今日的 YYYY-MM-DD 字符串或空"
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "asset_type"
            value: "nas"
- Input: "调度循环在 00:10 后进入当日采集窗口,对 resource_type=nas 执行一次原子认领"
- Output: "认领成功返回真;恰好提交 1 条 nas:collect_metrics 采集任务(days=2);日志记录任务标识与当日日期"
- State: "scheduler_state 中 nas 键的 last_date 推进为今日并持久化;任务队列新增一条待处理采集任务"
- Side-effect: "对 scheduler_state 集合执行一次 findOneAndUpdate 原子写(条件 last_date 小于今日)"

## Outcome "restart-same-day-no-reclaim"
- Preconditions: "当日日闸已被本进程或先前进程成功认领(scheduler_state 中 nas 键 last_date 等于今日)后服务进程重启,调度循环重新进入当日窗口"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "nas"
          - field: "last_date"
            value: "等于今日的 YYYY-MM-DD 字符串"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "重启后调度器再次尝试触发当日 NAS 采集"
- Output: "认领条件不匹配返回认领失败,静默跳过不产生错误;当日不再出现新的采集任务提交"
- State: "scheduler_state 的 last_date 保持为今日不变;任务队列中该资源类型的采集任务数保持不变"

## Outcome "concurrent-claim-lost"
- Preconditions: "多副本部署或手动任务与每日自动任务同日重叠;另一实例已在本实例之前原子认领当日 nas 键"
  fixture_spec:
    entities:
      - entity_type: "SchedulerState"
        min_count: 1
        field_constraints:
          - field: "resource_type"
            value: "nas"
          - field: "last_date"
            value: "等于今日的 YYYY-MM-DD 字符串(由竞争方写入)"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "两个实例同日并发执行认领操作"
- Output: "仅一个实例认领成功并提交采集任务;本实例认领失败不提交,无错误告警(正常竞争语义)"
- State: "scheduler_state 的 last_date 仅被推进一次;全天该资源类型恰好一条采集任务"

## Outcome "gate-write-failure-backoff"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_GATE_WRITE_RETRY(daily_gate.go:39-46,181-190)与 journey Step 1d:写失败走指数退避重试并升级告警,非仅记日志 -->
- Preconditions: "mongo 日闸写入失败(网络抖动/主从切换),认领写操作返回错误"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "scheduler_state 写路径被注入故障(连续写失败)"
        prerequisite_entity: "SchedulerState"
- Input: "调度循环触发当日认领,写操作失败"
- Output: "按 1 秒/2 秒/4 秒指数退避重试 3 次;重试耗尽后发出日闸写失败升级告警(共用日闸告警通道)并进入跨轮指数退避(1 分钟起、5 分钟封顶)"
- State: "scheduler_state 的 last_date 未被推进;当日任务未被提交;退避窗口内不重复提交任务"
- Side-effect: "重试耗尽时调用告警通道上报写失败(resource_type=nas, operation=write)"

## Journey Invariants

- 每日每「(account_id, fs_id, date)」至多一行;同日首写生效仅保护今日行,昨日行由次日补采覆盖更新后冻结跨日不可变(Asia/Shanghai 自然日)
- 所有落库容量字段以 GB 为唯一口径,字节到 GB 的换算只发生在采集边界,原始字节永不直写 GB 字段
- utilization 永不落库,任何读取路径均由 capacity/used 现场派生,且 capacity=0 时为 null(无 NaN、无 panic)
- 任何单厂商/单实例采集失败不得阻塞其他厂商/实例的采集与读取(尽力而为语义),且失败必须可观测(失败计数/末次错误/前端空态区分)
- NAS 界面数据一律以 `ecam_nas_metric` 指标表为唯一来源,资产表 capacity/used_capacity 不在 NAS 界面展示容量数值
- 当日(Asia/Shanghai 自然日)同一资源类型的采集任务至多提交一次;原子认领是提交的唯一入口
