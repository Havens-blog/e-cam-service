---
journey: "bind-failure-compensation"
step: 3
step-action: "映射状态迁移 active→orphan 入清理队列"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/bind-failure-compensation/journey.md
skip_eval: true
---

# Contract: bind-failure-compensation / Step 3: 映射状态迁移 active→orphan 入清理队列

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "orphan-transition-enqueued"
- Preconditions: "绑定失败的补偿链路执行中，映射记录可按云证书 ID 定位"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（迁移前）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "系统将该条目的 CloudCertMapping 记录由 active 迁移为 orphan 并加入清理队列"
- Output: "映射记录状态为 orphan（状态枚举仅 active/orphan 两态）；进入清理队列等待删除（按上传时间升序先进先出消费）；状态机与 aliyun/tencent 失败路径同构"
- State: "映射 UpdateStatus active→orphan；队列可见性就绪（按状态列表查询可取到）"
- Side-effect: "none"

## Outcome "compensation-delete-failure-keeps-orphan"
- Preconditions: "补偿/清理的云侧删除调用失败（限流/网络故障）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
    state_requirements:
      - description: "云侧删除 API 注入失败（限流或网络错误）"
        prerequisite_entity: "CloudAccount"
- Input: "系统记录补偿失败并重试"
- Output: "映射保留 orphan 状态并留在清理队列重试，不吞错、不误标已清理；单条失败不中断清扫（逐条隔离、首个错误聚合上报）；重复失败按去重键抑制重复告警，最终收敛或显式暴露未清理项"
- State: "映射 status=orphan 不变；清理结果记 Success=false 并产生运维告警（仅新失败）"
- Side-effect: "云删除 API 失败重试调用"

## Journey Invariants
- 绑定失败必经 CleanupOrphan 补偿，且补偿幂等：任何重试/并发路径不重复删除云资源
- 映射状态迁移单调可追溯（active→orphan），不存在 active 与 orphan 并存的同一云证书 ID 记录
- 清理队列最终收敛：每个 orphan 记录要么被删除、要么显式保留待重试，不允许静默丢失

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "orphan-transition: 迁移前 active；delete-failure: orphan"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "delete-failure 分支云侧删除 API 注入失败"
      prerequisite_entity: "CloudAccount"
```
