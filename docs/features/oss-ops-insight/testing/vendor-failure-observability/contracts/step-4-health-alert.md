---
journey: "vendor-failure-observability"
step: 4
step-action: "必达厂商持续失败触发健康告警"
generated: "2026-09-20"
skip_eval: true
sources:
  - docs/features/oss-ops-insight/testing/vendor-failure-observability/journey.md
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: vendor-failure-observability / Step 4: 必达厂商持续失败触发健康告警

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "health-alert-fired"
- Preconditions: "故障注入使某必达厂商(aliyun/huawei/aws)连续 3 天零成功写库;实盘存在至少 1 个 OSS bucket;健康告警通道已接通;全量采集运行(非手动局部运行)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "必达厂商之一"
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "该必达厂商近 3 天在 ecam_oss_metric 无任何成功写入行"
        prerequisite_entity: "OSSMetricRow"
- Input: "执行每日全量采集,观察告警通道"
- Output: "健康告警触发(AlertOSSZeroSuccess),明确指出厂商与零成功时间窗(近 3 天)"
- State: "告警随任务 Result 的 health_alerts 可见"
- Side-effect: "发送健康告警(共用 schedulerGateAlerter 通道)"

## Outcome "best-effort-no-alert"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4 预期「尽力而为厂商的失败不触发该健康告警」;Fact Table OSS_HEALTH_ALERT(oss_health_monitor.go)告警只针对必达厂商 -->
- Preconditions: "尽力而为厂商(tencent/volcengine)持续无指标能力或失败;必达厂商正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "尽力而为厂商"
- Input: "执行每日全量采集,观察告警通道"
- Output: "尽力而为厂商的失败/不支持不触发健康告警;无误报"
- State: "health_alerts 为空"
- Side-effect: "none"

## Outcome "alert-throttled-not-flooding"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4b 定义告警不洪泛;告警条件多轮持续满足时按节流策略发送,恢复后状态解除 -->
- Preconditions: "必达厂商持续零成功超过告警阈值;告警条件在多轮采集中持续满足"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "必达厂商之一"
- Input: "连续多轮采集观察告警发送"
- Output: "告警按合理节流策略发送,不逐轮洪泛重复告警;恢复成功后告警状态解除"
- State: "告警状态随恢复翻转(触发→解除)"
- Side-effect: "节流后的健康告警发送"

## Journey Invariants

- 健康告警只针对必达厂商的持续零成功;尽力而为厂商的已知限制不告警
- 告警按节流策略发送不洪泛,恢复后状态解除
