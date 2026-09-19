---
journey: "vendor-failure-observability"
step: 1
step-action: "弱厂商失败返回空不阻塞"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/vendor-failure-observability/journey.md
---

# Contract: vendor-failure-observability / Step 1: 弱厂商失败返回空不阻塞

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "租户下同时存在必达厂商(aliyun/huawei/aws)与尽力而为厂商(tencent/volcengine)的 NAS 实例与账号;尽力而为厂商适配器正常返回"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "一个必达厂商与一个尽力而为厂商"
      - entity_type: "NASInstance"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
      - entity_type: "CollectTask"
        min_count: 1
        field_constraints:
          - field: "type"
            value: "nas:collect_metrics"
- Input: "触发 nas:collect_metrics 采集,遍历实例命中尽力而为厂商"
- Output: "该适配器返回自身有效结果(含真实指标或空),其余厂商与全流程正常继续;单实例失败跳过继续"
- State: "两厂商实例各自产出指标结果;任务整体成功"
- Side-effect: "对实例调用厂商监控 API"

## Outcome "probe-unsupported-info"
- Preconditions: "尽力而为厂商适配器遇到「指标名/namespace 未知(探测不支持)」形态——如 volcengine 产品订阅未开通返回指标不存在"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "尽力而为厂商(tencent/volcengine)"
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "厂商 API 对全部候选指标返回不支持/不存在形态"
        prerequisite_entity: "NASInstance"
- Input: "触发采集并检查该实例的失败路径日志"
- Output: "适配器返回空结果加 nil 错误;打 INFO 级日志(探测不支持),不打 ERROR、不进失败计数"
- State: "该实例不产生指标行;任务 Result 的 no_metric_support 计数可见,不进 failures"
- Side-effect: "INFO 级日志留痕"

## Outcome "api-call-failure-error"
- Preconditions: "另一实例遇到「API 错误/超时/鉴权失败」形态的真实调用失败"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "NASInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "适配器调用注入网络错误/超时/鉴权失败"
        prerequisite_entity: "NASInstance"
- Input: "触发采集并检查该实例的失败路径日志"
- Output: "适配器返回错误;打 ERROR 级日志并携带 error 字段;与探测不支持路径在日志级别与内容上可分辨"
- State: "该实例不产生指标行;执行器失败计数累加,末次错误进入任务 Result 的 failures"
- Side-effect: "ERROR 级日志留痕(含错误详情)"

## Journey Invariants

- 任何单厂商/单实例失败只影响自身,全流程永不因尽力而为厂商失败而阻塞或报错
- 「探测不支持」与「调用失败」永远可分辨(INFO vs ERROR+error 字段);失败必须出现在任务 Result 中,绝不静默吞掉
- 探测不支持不计入失败计数(与真实失败在 Result 上可分辨)
