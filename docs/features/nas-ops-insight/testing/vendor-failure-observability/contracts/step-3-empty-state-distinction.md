---
journey: "vendor-failure-observability"
step: 3
step-action: "前端空态区分无数据与采集失败"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/vendor-failure-observability/journey.md
---

# Contract: vendor-failure-observability / Step 3: 前端空态区分无数据与采集失败

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "失败厂商实例窗口内无指标行且最近采集任务 Result 无失败(真实无数据形态);运营打开该实例抽屉监控 tab 与列表页运营卡"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
          - field: "result.failures"
            value: "空数组"
- Input: "运营打开实例监控 tab 与运营卡查看空态"
- Output: "空态呈现「无数据(指标真实为 0 或空)」分支;采集异常警示不出现;与「采集失败/未启用」的警示态、zero_exception 异常态三者可分辨"
- State: "无状态变更(展示派生自任务 Result 与指标行)"
- Side-effect: "none"

## Outcome "collect-failure-warning-shown"
- Preconditions: "最近采集任务 Result 失败计数大于 0(采集失败/未启用形态),窗口内指标行缺失"
  fixture_spec:
    entities:
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
          - field: "result.failures"
            value: "非空且 error_count 大于 0"
- Input: "运营查看该厂商实例空态与运营卡"
- Output: "采集异常时显示警示,而非与真实无数据相同的纯空;警示与「0 占位」判定互斥"
- State: "无状态变更"
- Side-effect: "none"

## Outcome "warning-cleared-after-recovery"
- Preconditions: "失败厂商次日恢复成功写库(窗口内出现成功落库行)"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "date"
            value: "今日或近日(恢复后的成功行)"
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "result.failures"
            value: "空数组(最近任务无失败)"
- Input: "运营查看该厂商实例空态与运营卡"
- Output: "空态恢复为正常数据态/0 占位语义,警示随失败计数清零解除,不残留过期警示"
- State: "无状态变更;警示判定以最近一次任务 Result 为准"
- Side-effect: "none"

## Journey Invariants

- 前端空态语义三分:真实无数据 / 采集失败或未启用 / 零容量异常(zero_exception),三者不得混同展示
- 「警示」判定永远优先于「0 占位」;未知(采集失败)不得伪装成真零容量
- 警示随失败计数清零解除,不残留过期警示
