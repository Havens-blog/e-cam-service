---
journey: "volcano-two-phase-replacement"
step: 2
step-action: "确认变更清单并配置分批灰度"
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

# Contract: volcano-two-phase-replacement / Step 2: 确认变更清单并配置分批灰度

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "变更清单已生成且含火山可执行项；提交的分批策略批次比例不超过灰度上限（单批占比不超过一半）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "items"
            value: "含至少 1 项 AutoChangeable=true 的火山项"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "volcano"
- Input: "运维确认清单并提交分批策略（批次比例 ≤50%，如 4 项分 2 批）"
- Output: "清单进入可执行状态，项目按批次划分，火山引用按云/产品通道分发命中火山部署器"
- State: "变更单状态由待确认转为可执行；批次划分固化"
- Side-effect: "none"

## Outcome "batch-ratio-rejected"
- Preconditions: "同 success 基线，但提交的分批参数批次比例超过灰度上限（大于一半）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "items"
            value: "含至少 1 项 AutoChangeable=true 的火山项"
- Input: "提交越界分批参数（批次比例 >50%）"
- Output: "请求被拦截拒绝（灰度上限 ≤50% 硬约束语义复用），呈报越界原因"
- State: "清单不进入执行态，批次划分不固化，变更单状态不变"
- Side-effect: "none"

## Journey Invariants
- 分批灰度 ≤50% 硬约束复用零新机制（MaxBatchRatioLimit=0.5）
- 清单确认不产生云侧写操作；执行写操作仅存在于后续批次执行步
