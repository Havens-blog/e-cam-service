---
journey: "multi-account-shared-fs"
step: 1
step-action: "三账号同 fs 并发采集"
generated: "2026-09-19"
skip_eval: true
sources:
  - docs/features/nas-ops-insight/testing/multi-account-shared-fs/journey.md
---

# Contract: multi-account-shared-fs / Step 1: 三账号同 fs 并发采集

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "同一物理 NAS 文件系统(相同 fs_id)被至少 3 个云账号同时纳管,三账号均属活跃账号集合,采集可触达"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "NASInstance"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
        field_constraints:
          - field: "fs_id"
            value: "三账号指向同一物理文件系统的相同 fs_id"
- Input: "触发 nas:collect_metrics 采集,执行器分别以三个账号身份调用厂商监控 API 采集同一 fs_id 的容量/用量"
- Output: "每个账号产出各自指标值(capacity/used_capacity 各自口径);任务整体成功"
- State: "三组采集结果进入落库阶段;账号互斥闸保证同账号采集互斥,不同账号可并发"
- Side-effect: "对每账号每实例调用厂商监控 API(外部网络调用)"

## Outcome "account-failure-isolated"
- Preconditions: "三账号中任一账号的适配器调用失败(宕机/鉴权失效/网络错误),其余账号采集正常"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "NASInstance"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "其中一个账号的适配器注入调用失败"
        prerequisite_entity: "CloudAccount"
- Input: "执行器按账号遍历采集,命中失败账号"
- Output: "失败账号仅返回自身空结果;其余两账号照常产出各自指标值;失败进入任务 Result 的 failures(账号维度失败计数与末次错误)"
- State: "失败账号不产出指标;其余账号采集结果完整;全流程不因单账号失败而失败"
- Side-effect: "失败路径打 ERROR 级日志并携带错误字段"

## Outcome "zero-capacity-rows-kept"
<!-- source: inferred -->
<!-- reasoning: Fact Table NAS_QC_GATE(nas_metric.go:95-97)与 vendor-failure journey Step 2d:华为/AWS 实盘 capacity=0 行不继承 CDN 全零跳过过滤,标记 zero_exception 后落库;多账号共享 fs 场景下该边界与跨账号隔离叠加,须确认零值行同样按账号独立保留 -->
- Preconditions: "三账号中某账号的厂商返回其视角 capacity=0,其余账号返回正常非零值"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
      - entity_type: "NASInstance"
        min_count: 3
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
    state_requirements:
      - description: "其中一个账号的厂商返回 capacity=0,其余账号正常"
        prerequisite_entity: "NASInstance"
- Input: "采集写入三账号各自当日行"
- Output: "capacity=0 行标记 qc_status=zero_exception 后照常按该账号落库,不拦截不跳过;三账号各留一行"
- State: "ecam_nas_metric 同 fs 同日存在三行,其一 qc_status=zero_exception"
- Side-effect: "none"

## Journey Invariants

- 唯一键 (account_id, fs_id, date) 之下,任何写入路径都不得让一个账号的行覆盖/删除另一账号的行
- 任何单账号失败只影响自身,不阻塞其余账号采集
- capacity=0 的异常行永远落库可见且按账号独立保留
