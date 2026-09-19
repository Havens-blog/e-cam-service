---
journey: "volcano-bind-failure-compensation"
step: 3
step-action: "幂等复验"
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

# Contract: volcano-bind-failure-compensation / Step 3: 幂等复验

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "double-call-same-result"
- Preconditions: "补偿清理已成功执行过一次（云侧孤儿已删除、映射已转 orphan）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
- Input: "重复调用 CleanupOrphan（双调用）"
- Output: "双调用同结果——第二次为幂等空成功（已删除即视为清理完成），无报错无状态翻转"
- State: "映射保持 orphan 不翻转；云侧无新增操作副作用"
- Side-effect: "none（幂等重放）"

## Outcome "manually-deleted-cloud-side"
- Preconditions: "清理队列含孤儿条目，但云侧证书已被控制台手动删除"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan"
    state_requirements:
      - description: "云侧对应证书已不存在（控制台手动删除）"
        prerequisite_entity: "CloudCertMapping"
- Input: "触发清理"
- Output: "幂等语义覆盖「不存在即成功」（火山删除 API not-found 归一成功），队列收敛，不误报失败"
- State: "映射/队列状态收敛，无错误落库"
- Side-effect: "none（云侧无可删对象）"

## Outcome "non-normalized-id-fail-fast"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示清理路由对非归一云证书 ID fail-fast（splitVolcanoCloudCertID 不通过即报错，volcano_deployer.go:1401-1403）——非归一 ID 流入清理队列是现实脏数据边界 -->
- Preconditions: "清理队列存在非 {product}:{id} 归一形态的云证书 ID 条目（缺前缀/未知前缀/空裸 ID）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "cloudCertID"
            value: "非归一形态（缺前缀或未知前缀）"
- Input: "触发清理"
- Output: "fail-fast 结构化失败（要求归一形态），不猜测路由目标"
- State: "该条目状态不变，等待人工/上游修正；不误删其他库证书"
- Side-effect: "none"

## Outcome "unknown-product-prefix"
<!-- source: inferred -->
<!-- reasoning: Fact Table 显示清理路由对合法归一形态但产品不在支持集的 ID 返回产品不支持哨兵（volcano_deployer.go:1427-1429 default 分支）——未知前缀是扩展窗口现实边界 -->
- Preconditions: "清理队列存在前缀不在 csv/cdn/waf/alb/nlb 支持集内的条目"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "cloudCertID"
            value: "未知产品前缀的归一形态 ID"
- Input: "触发清理"
- Output: "结构化失败（产品不支持哨兵），呈报可诊断原因"
- State: "该条目状态不变；不影响其他条目清理"
- Side-effect: "none"

## Journey Invariants
- CleanupOrphan 幂等：双调用同结果（清理队列重放安全）
- 补偿失败不影响既有失败原因呈报（静态 reason，云侧细节仅日志）
- 孤儿清理走同一映射状态机，火山不引入新状态
