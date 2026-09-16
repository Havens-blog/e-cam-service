---
journey: "multicloud-cert-replacement"
step: 7
step-action: "旧证书孤儿清理"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 7: 旧证书孤儿清理

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "变更单到达终态，终态收敛已将成功条目的旧证书映射 active→orphan 入清理队列；该旧证书无在途变更单占用且不在保护期"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "终态（completed 等）"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "protectUntil"
            value: "已过期或为空"
- Input: "清理队列消费（每日定时清扫或验证窗口终态后事件触发）删除云侧孤儿证书"
- Output: "已无引用的旧云证书被删除（华为 SCM / AWS ACM / Azure KV 按各自删除 API；云侧已不存在按幂等成功语义）；映射记录删除（cleaned 以删除承载）；变更闭环完成，全程未登录任何云控制台"
- State: "CloudCertMapping 行删除；清理结果记录 Action=cleanup 且 Success=true"
- Side-effect: "云删除 API 调用（经限流与有界退避）"

## Outcome "owning-order-occupancy-skip"
- Preconditions: "待清理旧证书仍被在途变更单占用（作为某 active 变更单的旧证书互斥令牌或其新证书）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "executing（占用该证书为旧证书或新证书）"
- Input: "系统执行孤儿清理判定并尝试删除"
- Output: "孤儿判定跳过不删除（占用门禁命中），证书保留待后续清理；队列状态收敛不误删"
- State: "映射保持 orphan 留队列；无云侧删除"
- Side-effect: "none"

## Outcome "protection-period-skipkeep"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_CLEANUP_GATES（orphan_cleanup_service.go:300-316：protectUntil > now → Action=skip_keep 且 Success=true 不删除）+ MC_ROLLBACK_PROTECT（rollback_service.go:369-393：回滚完成置 protectUntil=now+RollbackProtectDays=7 天）——保护期是独立于占用门禁的第三重防线 -->
- Preconditions: "旧证书台账记录保护期未到期（protectUntil 晚于当前时间，如回滚后的 7 天保护窗内）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "protectUntil"
            value: "晚于当前时间"
- Input: "清理队列消费执行删除判定"
- Output: "按保护期跳过保留（skip_keep 且记成功、不删除、不报错），证书保留待保护期结束后的清扫"
- State: "映射保持 orphan 留队列；证书保留"
- Side-effect: "none"

## Journey Invariants
- 三云替换语义与 aliyun/tencent 完全同构：失败状态、回滚、清理走同一状态机
- 清理队列最终收敛：每个 orphan 记录要么被删除、要么显式保留待重试，不允许静默丢失
- 云证书 ID 空间互斥；映射唯一性保持

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "orphan"
    - entity_type: "ChangeOrder"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "success: 终态；occupancy 分支: executing 占用该证书"
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "protectUntil"
          value: "success: 已过期/为空；protection 分支: 未到期"
```
