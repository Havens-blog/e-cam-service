---
journey: "three-cloud-product-matrix"
step: 1
step-action: "生成覆盖 9 组合的变更清单"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/three-cloud-product-matrix/journey.md
skip_eval: true
---

# Contract: three-cloud-product-matrix / Step 1: 生成覆盖 9 组合的变更清单

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "all-9-combos-executable"
- Preconditions: "9 个 cloud×product 组合（华为 cdn/waf/alb/nlb、AWS cdn/alb/nlb、Azure cdn/alb）在 done 快照中各有至少一条待替换证书引用；操作者为 ops_engineer"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 9
        field_constraints:
          - field: "cloud"
            value: "覆盖华为/AWS/Azure 三云与各自已注册产品的全部 9 组合"
      - entity_type: "CloudAccount"
        min_count: 3
        field_constraints:
          - field: "provider"
            value: "华为/AWS/Azure 各一"
- Input: "运维人员生成覆盖三云全部产品引用的变更清单"
- Output: "9 个 cloud×product 组合的引用均为可执行项（AutoChangeable=true），无一组合按 ERR_DISCOVERY_ONLY 标记 skipped（该分区已随三云部署器落地移除）"
- State: "变更单 Status=pending_confirm；9 个条目以 pending 入清单"
- Side-effect: "none"
- Invariants: "9 个组合共用同一五方法端口语义，云差异只在每云适配层归一"

## Outcome "empty-combo-produces-no-items"
<!-- source: inferred -->
<!-- reasoning: 条目按引用逐条构建（changelist_generator.go:292 buildChangeItems、:320-323 落库），组合无引用即无输入、自然不产出条目；journey 1b 的"跳过不产出条目"对应此行为 -->
- Preconditions: "9 组合中某产品当前无任何证书引用（快照中该组合零引用），其余组合引用齐备"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "cloud"
            value: "仅覆盖其余 8 个组合（某组合零引用）"
- Input: "运维人员生成变更清单"
- Output: "该组合不产出条目（静默无条目，非 skipped、非报错），不影响其余组合正常产出与执行"
- State: "变更单仅含其余组合条目"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效会话"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
    state_requirements:
      - description: "无有效会话"
        prerequisite_entity: "Certificate"
- Input: "POST /api/v1/certs/changes 无有效会话调用"
- Output: "HTTP 401 全局认证失败文案（非 cert 模块 Envelope）"
- State: "无状态变化，不创建变更单"
- Side-effect: "none"

## Journey Invariants
- 9 个 cloud×product 组合共用同一五方法端口语义与两段式编排，云差异只在每云适配层归一
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态
- 云证书 ID 形态按云固定：华为 SCM ID、AWS ACM ARN、Azure KV secret ID 引用

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "CertReference"
      min_count: 9
      field_constraints:
        - field: "cloud"
          value: "success: 覆盖 9 组合；empty-combo: 缺一个组合"
    - entity_type: "CloudAccount"
      min_count: 3
```
