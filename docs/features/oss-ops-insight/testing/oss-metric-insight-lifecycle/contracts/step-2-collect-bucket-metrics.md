---
journey: "oss-metric-insight-lifecycle"
step: 2
step-action: "按活跃账号遍历 bucket 采集容量/对象数"
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

# Contract: oss-metric-insight-lifecycle / Step 2: 按活跃账号遍历 bucket 采集容量/对象数

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "collected-all-active-accounts"
- Preconditions: "至少 1 个活跃云账号已纳管,且 ecam_instance 中该账号存在至少 1 个 asset_type=oss 的 bucket;必达厂商监控 API 实盘可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "必达厂商之一(aliyun/huawei/aws)"
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行 oss:collect_metrics 采集任务(days=2,采集区间 [昨日, 今日])"
- Output: "每个 bucket 产出当日容量(经字节→GB 归一化)与对象数指标;任务 Result 含 metrics_total 与 accounts 计数;容量值落在 [1MB, 1PB] 数量级门禁内"
- State: "采集结果在内存中按今日/昨日分流为待写行,尚未落库(落库见 Step 3)"
- Side-effect: "调用厂商监控 API(有厂商侧 API 配额消耗)"
- Invariants: "活跃账号口径以 ecam_instance 枚举存在至少 1 个 OSS bucket 为准,不依赖账号 EnableAutoSync 开关"

## Outcome "vendor-call-failure"
<!-- source: inferred -->
<!-- reasoning: Journey Step 2b 定义必达厂商调用失败;Fact Table OSS_CONCURRENCY_MODEL/OSS_FAILURES_SUMMARY(sync_oss_metrics.go:218-228,375-382)失败记 ERROR+error 字段+failures 计数,不阻塞其余厂商与账号 -->
- Preconditions: "某必达厂商(如 aliyun)的监控 API 调用失败;同租户下其他厂商账号正常可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "至少一个账号为故障必达厂商,至少一个账号为正常厂商"
      - entity_type: "OSSBucketAsset"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行 oss:collect_metrics 采集任务,遍历含故障厂商的全部活跃账号"
- Output: "故障厂商适配器只返回自身空结果;ERROR 日志 + error 字段记录;Result 的 failures 计数含该厂商/账号失败明细(provider/account_id/error_count/last_error);其他厂商与其他账号的 bucket 仍正常产出指标"
- State: "故障厂商的指标行不落库;正常厂商账号的指标照常进入待写行"
- Side-effect: "调用厂商监控 API;失败明细写入任务 Result"

## Outcome "vendor-no-metric-support"
<!-- source: inferred -->
<!-- reasoning: Journey Step 2c 定义尽力而为厂商无该指标;Fact Table OSS_NO_METRIC_SUPPORT_INFO(sync_oss_metrics.go:291-297)明确 INFO+空返回不算失败 -->
- Preconditions: "某尽力而为厂商(tencent/volcengine)的 OSS 适配器未实现指标查询能力(实盘不支持 OSS 容量指标)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "尽力而为厂商(tencent 或 volcengine)"
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行 oss:collect_metrics 采集任务,遍历到该尽力而为厂商账号"
- Output: "INFO 日志说明该厂商不支持指标查询;空返回;不计失败、不进 failures 计数;Result 的 no_metric_support 列表含该厂商;全流程不中断"
- State: "该厂商账号不产生指标行;其余账号采集不受影响"
- Side-effect: "none"

## Journey Invariants

- 任一厂商/账号/bucket 的失败只影响自身,绝不阻塞其他厂商/账号/bucket 的采集继续
- 活跃账号口径以 ecam_instance 枚举存在至少 1 个 OSS bucket 为准,不依赖 EnableAutoSync
- 「探测不支持」(INFO+空返回)与「调用失败」(ERROR+failures 计数)严格区分,不混淆失败口径
