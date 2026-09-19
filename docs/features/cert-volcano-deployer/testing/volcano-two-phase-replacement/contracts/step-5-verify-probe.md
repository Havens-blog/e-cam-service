---
journey: "volcano-two-phase-replacement"
step: 5
step-action: "验证窗口拨测并人工续批"
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

# Contract: volcano-two-phase-replacement / Step 5: 验证窗口拨测并人工续批

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "success"
- Preconditions: "非终批已执行绑定且处于验证窗口；ProbeDomains 配置含目标域名；线上证书引用已切换为新证书"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "phase"
            value: "验证窗口（非终批）"
      - entity_type: "ProbeDomainConfig"
        min_count: 1
        field_constraints:
          - field: "domains"
            value: "含绑定目标域名"
- Input: "对目标域名执行 ProbeDomains TLS 拨测并核对线上指纹"
- Output: "拨测线上指纹等于新证书指纹；批次按序推进直至终批完成"
- State: "批次推进至下一批或终批；变更单进度如实落库"
- Side-effect: "none（拨测只读）"

## Outcome "probe-mismatch-hold"
- Preconditions: "同 success 基线，但验证窗口内线上指纹仍等于旧证书指纹（绑定未生效或回滚已发生）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "phase"
            value: "验证窗口（非终批）"
    state_requirements:
      - description: "线上域名 TLS 拨测返回的指纹仍为旧证书指纹"
        prerequisite_entity: "ChangeList"
- Input: "拨测核对线上指纹"
- Output: "不推进终批，变更单停留验证窗口可人工介入（回滚路径见 journey volcano-rollback-verify-window）"
- State: "批次不推进，变更单停留验证窗口态"
- Side-effect: "none"

## Journey Invariants
- 验证窗口拨测云无关（ProbeDomains 按域名），复用零新机制
- 拨测不符不推进终批——回滚入口仅在异常时使用
