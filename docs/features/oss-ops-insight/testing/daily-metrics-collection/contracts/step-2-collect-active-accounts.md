---
journey: "daily-metrics-collection"
step: 2
step-action: "按活跃账号遍历 bucket 采集"
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

# Contract: daily-metrics-collection / Step 2: 按活跃账号遍历 bucket 采集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "two-day-window-collected"
- Preconditions: "至少 1 个活跃云账号已纳管且 ecam_instance 中存在至少 1 个 OSS bucket;厂商监控 API 可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行 oss:collect_metrics 采集任务,采集区间取 [昨日, 今日]"
- Output: "每个 bucket 产出昨日与今日两组指标值;任务 Result 含 date_range、metrics_total、accounts"
- State: "采集结果按今日/昨日分流为待写行(落库见 Step 3)"
- Side-effect: "调用厂商监控 API(账号级互斥 + bucket 有界并发 5,复用 nasAccountGate 模式)"
- Invariants: "单厂商/单 bucket 失败只影响自身,不阻塞其余继续"

## Outcome "single-failure-isolated"
<!-- source: inferred -->
<!-- reasoning: Fact Table OSS_CONCURRENCY_MODEL(sync_oss_metrics.go:171-209,309-338)账号互斥+有界并发下单 bucket 失败计 -1 不中断;Journey 预期「单厂商/单 bucket 失败只影响自身」需显式守护 -->
- Preconditions: "遍历中某个 bucket 的厂商查询或写库失败;同账号其余 bucket 及其他账号正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
      - entity_type: "OSSBucketAsset"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行 oss:collect_metrics 采集任务,含一个必然失败的 bucket"
- Output: "失败 bucket 计入 failed_buckets 与 failures 明细;其余 bucket 照常产出两组指标;全流程不中断"
- State: "失败 bucket 不产生待写行;正常 bucket 待写行完整"
- Side-effect: "none"

## Journey Invariants

- 活跃账号口径 = ecam_instance 枚举存在至少 1 个 OSS bucket,不依赖 EnableAutoSync 开关
- 任一厂商/账号/bucket 的失败只影响自身,绝不阻塞其他厂商/账号/bucket 的采集继续
- 账号级互斥:同一账号同时仅放行一个采集任务
