---
journey: "volcano-dispatch-visibility"
step: 3
step-action: "执行分发命中火山部署器"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-dispatch-visibility/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-dispatch-visibility / Step 3: 执行分发命中火山部署器

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "routed-to-volcano-deployer"
- Preconditions: "含火山项的变更单已确认可执行；火山部署器已注册四产品"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "items"
            value: "含 Target.Cloud=volcano 的可执行项"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
- Input: "确认并执行含火山项的变更单"
- Output: "火山目标经云/产品通道分发命中火山部署器（四产品逐项可见），不落入其他云部署器；执行进度按 item 如实可见"
- State: "火山项执行状态按 item 如实落库"
- Side-effect: "云侧写操作（属于执行面，归 journey volcano-two-phase-replacement 覆盖）"

## Outcome "non-volcano-no-misroute"
- Preconditions: "变更单混合火山与五云项且均已确认"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "items"
            value: "混合火山与五云目标"
- Input: "执行变更单"
- Output: "五云项命中各自既有部署器，火山项命中火山部署器，通道互不误路由"
- State: "各云项执行状态独立如实落库"
- Side-effect: "云侧写操作（按各自部署器通道）"

## Outcome "unregistered-target-cloud-error"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示执行通道按注册部署器分发（RegisterDeployer per 云+产品），未注册云/产品组合无命中目标——分发空转是装配异常的现实边界，应结构化报错而非 panic 或误路由 -->
- Preconditions: "变更单含目标云/产品组合在执行通道中无对应部署器注册"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "items"
            value: "含未注册云/产品组合的目标项"
- Input: "执行该变更单"
- Output: "结构化失败（无可用部署器），呈报可诊断原因，服务不 panic 不误路由到其他云"
- State: "该项失败落库，其余项不受影响"
- Side-effect: "none"

## Journey Invariants
- 通道分发按云/产品精确路由，跨云不误路由
- 执行进度按 item 如实可见，失败可诊断
