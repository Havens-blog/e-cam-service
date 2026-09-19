---
journey: "volcano-two-phase-replacement"
step: 6
step-action: "旧证书孤儿清理"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-two-phase-replacement/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-two-phase-replacement / Step 6: 旧证书孤儿清理

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "终批完成且验证通过；映射队列存在旧证书孤儿条目（替换前旧云证书 ID）"
  fixture_spec:
    entities:
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "orphan（清理队列）"
- Input: "触发旧证书孤儿清理（CleanupOrphan 按归一前缀路由对应产品库删除 API）"
- Output: "旧证书孤儿按映射队列清理成功（火山侧经对应产品库删除 API，幂等）"
- State: "云侧孤儿证书删除；映射/台账状态收敛无残留"
- Side-effect: "云侧写操作：删除旧证书（按前缀路由 csv/cdn/waf/alb-nlb 对应删除 API）"

## Outcome "empty-cleanup-idempotent"
- Preconditions: "首次替换，映射队列无孤儿条目"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
    state_requirements:
      - description: "映射仓储中无 orphan 状态条目（清理队列为空）"
        prerequisite_entity: "ChangeList"
- Input: "触发孤儿清理"
- Output: "空清理幂等成功，无报错"
- State: "无状态变化，无污染"
- Side-effect: "none"

## Outcome "rerun-idempotent"
- Preconditions: "批次已全部 success 且清理已执行过（重复触发同批执行）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "batch status"
            value: "全部 success"
- Input: "重复触发同批执行"
- Output: "幂等收敛——不重复上传（不产生重复库条目）、不重复绑定，状态与首次执行同结果"
- State: "云证书库不新增条目；映射不重复落行；item 状态不变"
- Side-effect: "none（幂等重跑不产生新云侧写操作）"

## Journey Invariants
- 孤儿清理走同一映射状态机（active→orphan→清理队列），火山不引入新状态
- CleanupOrphan 对已删除证书幂等成功（清理队列重放安全）
- 云侧错误细节不进响应仅日志
