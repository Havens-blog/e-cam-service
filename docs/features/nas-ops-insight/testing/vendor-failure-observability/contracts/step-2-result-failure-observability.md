---
journey: "vendor-failure-observability"
step: 2
step-action: "失败计数与末次错误进入任务 Result"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/vendor-failure-observability/journey.md
---

# Contract: vendor-failure-observability / Step 2: 失败计数与末次错误进入任务 Result

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "一次采集执行中存在失败厂商(调用失败形态),任务执行完毕;运营可查询任务 Result"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "一个失败厂商与一个正常厂商"
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
          - field: "status"
            value: "已执行完成"
- Input: "运营查看本次执行器任务 Result"
- Output: "Result 按厂商/账号维护失败计数与末次错误(failures 数组含 provider、account_id、error_count、last_error),失败厂商与成功厂商区分可查"
- State: "任务 Result 已持久化并可查询;失败信息不随任务结束丢失"
- Side-effect: "none"

## Outcome "zero-capacity-row-visible"
- Preconditions: "某实例厂商返回 capacity=0(CDN 执行器存在「当日全零即跳过不写库」过滤,NAS 不继承)"
  fixture_spec:
    entities:
      - entity_type: "NASMetric"
        min_count: 1
        field_constraints:
          - field: "capacity"
            value: "0"
          - field: "qc_status"
            value: "zero_exception"
- Input: "采集写入该行并查看 Result 与落库结果"
- Output: "capacity=0 异常行标记 qc_status=zero_exception 后落库可见;metrics_total 计入该行;不因全零被过滤"
- State: "ecam_nas_metric 存在该 zero_exception 行(否则华为/AWS 首日整表静默为空)"
- Side-effect: "none"

## Outcome "mandatory-zero-success-alert"
- Preconditions: "某必达厂商(aliyun/huawei/aws,实盘存在至少 1 个 NAS 实例)连续 3 天(默认窗口 [今日-2, 今日])当日零成功写库行;执行器每日健康检查以全量运行判定触发"
  fixture_spec:
    entities:
      - entity_type: "NASInstance"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "必达厂商之一"
    state_requirements:
      - description: "该厂商在健康窗口内 ecam_nas_metric 行数为 0 且实例数大于等于 1;本次运行为全量(无 account_id/provider 参数限定)"
        prerequisite_entity: "NASInstance"
- Input: "执行器每日健康检查运行"
- Output: "触发升级告警(直落 critical 级,与日闸写失败共用同一告警通道);告警 Source 标识厂商健康监控来源"
- State: "告警事件落库可见;Result 的 health_alerts 数组含该厂商条目"
- Side-effect: "通过共用的告警通道发出 critical 告警"

## Outcome "no-instance-no-alert"
- Preconditions: "某必达厂商当前在 ecam_instance 枚举中 NAS 实例数为 0(前置条件不满足)"
  fixture_spec:
    entities:
      - entity_type: "NASInstance"
        min_count: 0
    state_requirements:
      - description: "目标必达厂商全租户 NAS 实例枚举数为 0"
        prerequisite_entity: "NASInstance"
- Input: "执行器健康检查运行"
- Output: "不触发零成功告警(前置条件不满足),避免稳定误报造成告警疲劳"
- State: "无告警事件产生"
- Side-effect: "none"

## Journey Invariants

- 失败必须出现在任务 Result 中(failures 数组),绝不静默吞掉
- 「探测不支持」与「调用失败」在 Result 与日志上可分辨
- 零成功告警只对「实盘存在至少 1 个 NAS 实例的必达厂商」且全量运行时生效
- capacity=0 的异常行永远落库可见,不受任何「全零跳过」类过滤影响
