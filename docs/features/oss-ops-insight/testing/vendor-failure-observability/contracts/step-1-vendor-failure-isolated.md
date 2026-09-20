---
journey: "vendor-failure-observability"
step: 1
step-action: "必达厂商适配器调用失败不阻塞全流程"
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

# Contract: vendor-failure-observability / Step 1: 必达厂商适配器调用失败不阻塞全流程

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "vendor-failure-isolated"
- Preconditions: "采集执行器已注册;故障注入使某必达厂商(如 aliyun)监控 API 调用失败;其他厂商与其他账号可用"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "含故障必达厂商与正常厂商各至少一个"
      - entity_type: "OSSBucketAsset"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行器完成一轮全量采集"
- Output: "故障适配器只返回自身空;ERROR 日志 + error 字段记录;其他厂商与其他账号的采集正常继续,全流程不中断"
- State: "故障厂商指标不落库;正常厂商照常落库"
- Side-effect: "失败计入任务 Result 的 failures 汇总"

## Outcome "single-account-failure-isolated"
<!-- source: inferred -->
<!-- reasoning: Journey Step 1b 定义单账号失败不阻塞同厂商其他账号;Fact Table OSS_CONCURRENCY_MODEL 账号级互斥+失败互不传染 -->
- Preconditions: "同一必达厂商下 account A 的凭证失效、account B 正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "同一厂商"
      - entity_type: "OSSBucketAsset"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "执行器按账号级互斥遍历采集"
- Output: "仅 account A 计入失败明细;account B 正常落库;账号级失败互不传染"
- State: "A 无指标行;B 指标行正常落库"
- Side-effect: "none"

## Journey Invariants

- 任一厂商/账号的失败只影响自身,绝不阻塞其他厂商/账号/bucket 的采集继续
- 所有适配器失败必须可观测:error 字段或 Result["failures"] 至少其一可见,绝不静默吞错
