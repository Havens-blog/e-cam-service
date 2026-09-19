---
journey: "daily-metrics-collection"
step: 2
step-action: "按活跃账号遍历实例采集"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/daily-metrics-collection/journey.md
---

# Contract: daily-metrics-collection / Step 2: 按活跃账号遍历实例采集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "认领已成功且采集任务已提交执行;活跃账号下存在 NAS 实例且适配器按实例所在 region 可调用厂商监控 API"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "region"
            value: "任意合法地域字符串"
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
- Input: "执行器遍历活跃账号下的 NAS 实例,采集区间取 [昨日, 今日](days=2,补昨日完整行+今日初态)"
- Output: "每个实例产出昨日与今日两组指标值;任务执行成功且 metrics_total 大于 0"
- State: "采集结果进入落库阶段(Step 3);账号互斥闸对同账号并发采集互斥"
- Side-effect: "对每实例调用厂商监控 API(外部网络调用)"

## Outcome "vendor-failure-isolated"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_RESULT_KEYS(sync_nas_metrics.go:261-269)与 journey Step 2 期望「单厂商/单实例失败只影响自身,不阻塞继续」;厂商失败路径由 vendor-failure-observability journey 细化,此处作为每日采集主链的隔离性边界 -->
- Preconditions: "某厂商适配器对该账号实例调用失败(宕机/鉴权失效),其余厂商正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "NASInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "其中一家厂商适配器注入调用失败"
        prerequisite_entity: "NASInstance"
- Input: "执行器遍历采集,命中失败厂商实例"
- Output: "失败厂商仅返回自身空结果;其余厂商实例照常产出昨日与今日两组指标;任务 Result 的 failures 含该厂商失败计数与末次错误"
- State: "失败厂商实例不产出指标;其余实例采集结果完整"
- Side-effect: "失败路径打 ERROR 级日志并携带错误字段"

## Outcome "account-without-nas-recorded"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_RESULT_KEYS 含 accounts_without_nas 键(sync_nas_metrics.go:266);无实例账号进 Result 可观测而非静默忽略,属遍历边界 -->
- Preconditions: "租户下存在已纳管但没有任何 NAS 实例的云账号(非活跃账号)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "nas_instance_count"
            value: "0"
- Input: "执行器遍历账号发现该账号名下无 NAS 实例"
- Output: "跳过该账号不调用厂商 API;该账号记入任务 Result 的 accounts_without_nas 列表"
- State: "该账号不产生任何指标行;任务整体仍成功"
- Side-effect: "none"

## Journey Invariants

- 认领成功是提交采集任务的唯一前提;采集区间固定 [昨日, 今日](Asia/Shanghai)
- 任何单厂商/单实例失败只影响自身,不阻塞其余实例与全流程
- 无实例账号进 accounts_without_nas 可观测,不静默忽略
