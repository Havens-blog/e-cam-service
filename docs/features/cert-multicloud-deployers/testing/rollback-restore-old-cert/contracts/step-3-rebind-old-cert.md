---
journey: "rollback-restore-old-cert"
step: 3
step-action: "BindResource 反绑回旧云证书"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/rollback-restore-old-cert/journey.md
skip_eval: true
---

# Contract: rollback-restore-old-cert / Step 3: BindResource 反绑回旧云证书

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "rebind-success"
- Preconditions: "回滚预检已通过；目标资源与旧云证书 ID 就绪"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "success（回滚执行中）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "系统将目标资源重新绑定到旧云证书 ID（与正向替换同一 BindResource API 与产品分支语义）"
- Output: "资源恢复引用旧证书，绑定结果显式成功；条目收敛 rolled_back"
- State: "ChangeItem rolled_back；变更单收敛（全部回滚 → rolled_back，部分 → partial_completed 保持可再入）；被替换的新证书映射 active→orphan 入清理队列"
- Side-effect: "云绑定 API 调用；新证书映射转 orphan"

## Outcome "repeated-rollback-idempotent"
- Preconditions: "回滚已完成后运维人员再次发起同一回滚（所选条目均已 rolled_back）"
  fixture_spec:
    entities:
      - entity_type: "ChangeOrder"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "rolled_back 或 partial_completed"
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "rolled_back"
- Input: "系统处理重复回滚请求"
- Output: "幂等处理：范围准入仅 success 条目 → 无可回滚范围返回 400，不产生重复绑定副作用；并发重复回滚经 CAS 竞争由一方胜出，失败方静默让位（不重复告警/审计）；结果与首次回滚一致（资源引用旧证书）"
- State: "终态唯一：资源仍引用旧证书，条目保持 rolled_back"
- Side-effect: "none"

## Outcome "multi-resource-all-rebound"
- Preconditions: "替换时新证书被绑定到同一变更单的多个资源条目（多条 success 条目一并纳入回滚范围）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "均为 success"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "系统执行批量回滚反绑"
- Output: "所有已绑定资源条目均反绑回旧证书，无遗漏；不出现部分资源新证书、部分旧证书的混合态；任一条目失败则该条目 rollback_failed 并立即告警，变更单收敛 rollback_failed（不静默混合）"
- State: "逐条目隔离收敛（rolled_back / rollback_failed）；变更单按失败聚合规则收敛"
- Side-effect: "逐条云绑定调用"

## Journey Invariants
- 回滚按旧云证书 ID 反绑，与正向替换复用同一 BindResource/绑定 API，不新增云特定回滚机制
- 回滚幂等：重复回滚不产生重复副作用，终态唯一（资源引用旧证书）
- 任一条目回滚失败显式告警，不允许部分混合终态静默通过

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 2
      field_constraints:
        - field: "status"
          value: "rebind-success: success；repeated: rolled_back；multi-resource: 均 success"
    - entity_type: "ChangeOrder"
      min_count: 1
    - entity_type: "CloudAccount"
      min_count: 1
```
