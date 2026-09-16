---
journey: "bind-failure-compensation"
step: 4
step-action: "清理队列执行删除云侧孤儿证书"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/bind-failure-compensation/journey.md
skip_eval: true
---

# Contract: bind-failure-compensation / Step 4: 清理队列执行删除云侧孤儿证书

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "cleanup-success"
- Preconditions: "映射 status=orphan；其证书不被在途变更单占用且不在保护期"
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
            value: "已过期或为空"
- Input: "运维人员等待/触发清理队列消费，删除云侧孤儿证书"
- Output: "云侧孤儿证书被删除，映射记录删除（队列与映射状态收敛一致），不留半成品资源；变更单整体保持失败可重跑语义"
- State: "CloudCertMapping 行删除；清理结果 Action=cleanup 且 Success=true"
- Side-effect: "云删除 API 调用"

## Outcome "rerun-after-compensation-new-ids"
- Preconditions: "条目补偿完成（orphan 已清理），运维人员对失败批次重新发起执行（生成新变更单）"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "pending_confirm（新变更单）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "运维人员对失败批次重新发起变更单生成与执行"
- Output: "重跑重新上传/绑定产生新的云证书 ID 与 active 映射，不复用已清理的 orphan 记录；新旧映射不冲突（同键覆盖或新指纹新键）"
- State: "新变更单与条目创建并执行；新映射 status=active"
- Side-effect: "新一轮云上传/绑定调用"

## Outcome "already-deleted-success"
- Preconditions: "清理队列消费时云侧证书已被人工删除"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
    state_requirements:
      - description: "云侧目标证书已不存在（人工删除，含 Azure 软删除态）"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行清理删除"
- Output: "判定已不存在为清理成功语义（各云未找到均按幂等成功返回），不报错不重试死循环，队列状态收敛"
- State: "映射记录删除；清理结果 Success=true"
- Side-effect: "云删除 API 幂等重放调用"

## Outcome "sweep-isolation"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_SWEEP_ISOLATION（orphan_cleanup_service.go:176-186, 207-215：单条失败不中断清扫、首个错误聚合上报）——多记录清扫的失败隔离边界 -->
- Preconditions: "清扫批次中某条 orphan 记录删除失败，其余记录可正常删除"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "均为 orphan"
    state_requirements:
      - description: "其中一条的云删除 API 注入失败，其余正常"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行一轮孤儿清扫（多条记录）"
- Output: "失败记录保留 orphan 待重试并聚合上报，其余记录照常删除收敛；清扫不因单条失败中断"
- State: "失败项映射保留 orphan；成功项映射删除"
- Side-effect: "逐条云删除 API 调用"

## Journey Invariants
- 绑定失败必经 CleanupOrphan 补偿，且补偿幂等：任何重试/并发路径不重复删除云资源
- 清理队列最终收敛：每个 orphan 记录要么被删除、要么显式保留待重试，不允许静默丢失
- 失败后重跑不复用已清理的 orphan 记录，新旧映射不冲突

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudCertMapping"
      min_count: 2
      field_constraints:
        - field: "status"
          value: "orphan"
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "protectUntil"
          value: "已过期或为空"
    - entity_type: "CloudAccount"
      min_count: 1
```
