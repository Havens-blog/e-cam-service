---
journey: "vendor-failure-observability"
step: 3
step-action: "失败汇总入 Result[\"failures\"]"
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

# Contract: vendor-failure-observability / Step 3: 失败汇总入 Result["failures"]

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "failures-summary-locatable"
- Preconditions: "一轮采集结束;该轮中存在厂商/账号维度失败(如某必达厂商调用失败)"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "含故障账号"
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "查看执行器 Result 的 failures 汇总"
- Output: "Result[\"failures\"] 含 provider/account_id/error_count/last_error 维度,可定位到具体厂商与账号的失败明细;同时含 failed_buckets 计数"
- State: "失败明细随任务 Result 持久化可查"
- Side-effect: "none"
- Invariants: "失败明细可定位到厂商+账号粒度"

## Outcome "no-failure-empty-summary"
<!-- source: inferred -->
<!-- reasoning: 与失败场景互斥的对照——全部成功时 failures 为空且不虚报;Journey Invariants 要求口径不混淆 -->
- Preconditions: "一轮全量采集全部成功,无任何厂商/账号/bucket 失败"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "查看执行器 Result 的 failures 汇总"
- Output: "failures 为空列表;no_metric_support 与 accounts_without_oss 如实反映;不虚报失败"
- State: "任务 Result 无失败明细"
- Side-effect: "none"

## Journey Invariants

- 所有适配器失败必须可观测:error 字段或 Result["failures"] 至少其一可见,绝不静默吞错
- 失败明细含 provider/account_id/error_count/last_error,可定位到厂商+账号粒度
