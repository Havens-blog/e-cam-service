---
journey: "volcano-bind-failure-compensation"
step: 1
step-action: "绑定失败落账"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-bind-failure-compensation/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-bind-failure-compensation / Step 1: 绑定失败落账

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "bind-failure-static-reason"
- Preconditions: "两段式第一段已成功：云证书已上传火山产品库、映射落 active；第二段绑定调用可注入失败"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
    state_requirements:
      - description: "fake SDK 桩注入第二段绑定错误"
        prerequisite_entity: "CloudCertMapping"
- Input: "触发批次执行（注入绑定失败）"
- Output: "item 状态失败，错误呈报为静态 reason（云侧错误细节仅日志不进响应），编排立即进入补偿分支"
- State: "失败 item 状态落库；映射仍为 active（等待补偿转 orphan）"
- Side-effect: "none（绑定失败本身无新云侧写操作）"

## Outcome "batch-item-isolation"
- Preconditions: "同 success 基线，但同批含多个产品项且仅一项绑定失败"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "仅其中一个产品的绑定调用失败，其余正常"
        prerequisite_entity: "CloudCertMapping"
- Input: "执行批次"
- Output: "失败项补偿独立完成，其余项不受牵连（批次内失败隔离，对齐 journey volcano-two-phase-replacement Step 4b）"
- State: "失败项与成功项状态各自如实落库，互不污染"
- Side-effect: "仅失败项进入补偿分支"

## Outcome "cert-product-mismatch"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示绑定前校验证书前缀与目标产品同库（errVolcanoBindCertProductMismatch，volcano_deployer.go:623；alb/nlb 互跨除外）——跨产品误配是绑定层现实拒绝路径 -->
- Preconditions: "绑定调用的云证书 ID 前缀与目标产品不符（如 cdn 证书绑 waf 资源，非 alb/nlb 互跨形态）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "cloudCertID"
            value: "前缀与目标绑定产品不同库（非 alb/nlb 互跨）"
- Input: "对该目标执行第二段绑定"
- Output: "结构化失败（绑定产品不匹配错误），不发起云侧绑定调用"
- State: "无线上资源状态变化；item 失败落库"
- Side-effect: "none"

## Outcome "csv-cert-not-bindable"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示 csv 统一库实例不可直接绑定产品资源（ErrVolcanoCSVCertNotBindable 哨兵，volcano_deployer.go:615-617）——统一库证书流入绑定路径是产品库独立形态下的核心拒绝边界 -->
- Preconditions: "绑定调用的云证书 ID 为 csv 统一证书库前缀（非产品库证书）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "cloudCertID"
            value: "csv 前缀"
- Input: "以 csv 统一库证书 ID 对任一产品资源执行绑定"
- Output: "结构化失败（csv 统一库证书不可直接绑定产品资源的哨兵错误），fail-fast 不猜测不静默降级"
- State: "无线上资源状态变化；item 失败落库"
- Side-effect: "none"

## Outcome "empty-resource-id-rejected"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示 BindResource 对空 resourceID 显式拒绝（volcano_deployer.go:592-594）——空资源 ID 是入参校验现实边界 -->
- Preconditions: "绑定目标资源 ID 为空白"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
- Input: "以空资源 ID 执行第二段绑定"
- Output: "结构化失败（要求非空资源 ID），不发起云侧绑定调用"
- State: "无状态变化"
- Side-effect: "none"

## Journey Invariants
- 绑定失败必触发补偿，不留悬空 active 映射
- 云侧错误细节不进响应仅日志（静态 reason）
- 绑定目标产品与云证书前缀须同库（alb/nlb 互跨除外），csv 统一库不可直接绑定
