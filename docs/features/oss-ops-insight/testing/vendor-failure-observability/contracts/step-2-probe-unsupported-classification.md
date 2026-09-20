---
journey: "vendor-failure-observability"
step: 2
step-action: "尽力而为厂商探测不 supported 归类为非失败"
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

# Contract: vendor-failure-observability / Step 2: 尽力而为厂商探测不 supported 归类为非失败

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "probe-unsupported-info-not-failure"
- Preconditions: "故障场景设为尽力而为厂商(如 tencent)实盘无 OSS 容量指标(探测不支持);采集执行器已注册"
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
- Input: "执行器采集该尽力而为厂商的 bucket"
- Output: "INFO 日志 + 空返回;不算失败、不进 failures 计数;与「调用失败」(ERROR+计数)明确区分"
- State: "该厂商无指标行;其余厂商采集不受影响"
- Side-effect: "none"
- Invariants: "「探测不支持」与「调用失败」两类路径严格区分,不混淆失败口径"

## Outcome "recovery-success-counter-reset"
<!-- source: inferred -->
<!-- reasoning: Journey Step 2b 定义失败后恢复成功;失败计数按轮次归位,不无限累计历史失败,指标不丢日 -->
- Preconditions: "厂商 API 短暂故障后恢复可用;上一轮存在失败明细"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "OSSBucketAsset"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "下一轮采集执行"
- Output: "恢复后正常落库;failures 计数按轮次归位(不无限累计历史失败);指标不丢日(缺失日由补采/data_status 覆盖)"
- State: "本轮 failures 为空;缺失日指标被补齐"
- Side-effect: "none"

## Outcome "probe-attribution-recorded"
<!-- source: inferred -->
<!-- reasoning: Journey Step 4c 定义探测归因记录(订阅未开通 vs 指标不存在);归因显式记录不静默吞掉原因 -->
- Preconditions: "尽力而为厂商指标不可用,根因为「指标订阅未开通」或「指标不存在」"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "尽力而为厂商"
- Input: "查看探测/失败归因记录"
- Output: "归因被显式记录(订阅未开通记「二期补+开通路径」);不伪装数据、不静默吞掉原因"
- State: "归因信息可查询(日志/记录),不产生伪装指标行"
- Side-effect: "none"

## Journey Invariants

- 「探测不支持」(INFO+空返回)与「调用失败」(ERROR+failures 计数)严格区分,不混淆失败口径
- 探测归因显式记录,不伪装数据、不静默吞掉原因
