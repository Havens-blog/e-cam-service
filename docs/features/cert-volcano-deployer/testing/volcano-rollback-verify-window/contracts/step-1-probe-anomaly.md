---
journey: "volcano-rollback-verify-window"
step: 1
step-action: "验证窗口拨测发现异常"
generated: "2026-09-19"
sources:
  - docs/features/cert-volcano-deployer/testing/volcano-rollback-verify-window/journey.md
skip_eval: true
anchors:
  api:
    endpoint: ""
    method: ""
    content_type: ""
    auth_required:
last_anchor_sync: ""
---

# Contract: volcano-rollback-verify-window / Step 1: 验证窗口拨测发现异常

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "mismatch-detected"
- Preconditions: "变更单处于验证窗口（某批次绑定已执行，非终态）；ProbeDomains 配置含目标域名"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "phase"
            value: "验证窗口"
      - entity_type: "ProbeDomainConfig"
        min_count: 1
        field_constraints:
          - field: "domains"
            value: "含绑定目标域名"
    state_requirements:
      - description: "线上域名 TLS 拨测返回的指纹与新证书指纹不符"
        prerequisite_entity: "ChangeList"
- Input: "验证窗口内对目标域名执行 ProbeDomains TLS 拨测"
- Output: "拨测线上指纹不等于新证书指纹（仍为旧指纹或不符合预期），变更单停留验证窗口，回滚入口可用"
- State: "变更单停留验证窗口态，批次不推进"
- Side-effect: "none（拨测只读）"

## Outcome "probe-pass-no-rollback"
- Preconditions: "同 mismatch-detected 基线，但线上指纹已等于新证书指纹"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "phase"
            value: "验证窗口"
    state_requirements:
      - description: "线上域名 TLS 拨测返回的指纹等于新证书指纹"
        prerequisite_entity: "ChangeList"
- Input: "确认验证窗口通过并续批/终批"
- Output: "不触发回滚，批次正常推进（回滚入口仅在异常时使用，语义不误触）"
- State: "批次推进至下一批或终批"
- Side-effect: "none"

## Outcome "probe-failure-fail-safe"
<!-- source: inferred -->
<!-- reasoning: 拨测通道本身失败（网络/超时）时无法证明新指纹已生效——按 fail-safe 保守侧应与指纹不符同待遇停留窗口，不应推进批次 -->
- Preconditions: "变更单处于验证窗口，但拨测调用本身失败（网络异常或探测服务不可用）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "phase"
            value: "验证窗口"
    state_requirements:
      - description: "拨测服务对目标域名返回探测错误（非指纹结果）"
        prerequisite_entity: "ChangeList"
- Input: "执行拨测"
- Output: "探测错误按保守侧处理——不推进批次，停留验证窗口可重试拨测"
- State: "批次不推进，变更单停留验证窗口态"
- Side-effect: "none"

## Journey Invariants
- 验证窗口拨测云无关（ProbeDomains 按域名），回滚语义与五云同一状态机
- 拨测不符或不确定均不推进批次（fail-safe 保守侧）
