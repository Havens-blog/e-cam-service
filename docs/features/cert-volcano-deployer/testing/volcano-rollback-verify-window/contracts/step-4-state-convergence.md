---
journey: "volcano-rollback-verify-window"
step: 4
step-action: "回滚后状态收敛"
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

# Contract: volcano-rollback-verify-window / Step 4: 回滚后状态收敛

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

## Outcome "converged-truthful-terminal"
- Preconditions: "回滚执行完毕（全部成功或部分失败）"
  fixture_spec:
    entities:
      - entity_type: "ChangeList"
        min_count: 1
        field_constraints:
          - field: "phase"
            value: "回滚后"
      - entity_type: "CloudCertMapping"
        min_count: 1
- Input: "核对变更单/映射终态"
- Output: "回滚后变更单终态如实（回滚成功/部分失败），映射不因回滚产生重复行"
- State: "变更单终态与各产品恢复结果一致；映射行数与回滚前持平（无重复行）"
- Side-effect: "none"

## Outcome "re-replacement-capability-preserved"
<!-- source: inferred -->
<!-- reasoning: Journey 不变量「回滚不破坏后续替换能力」需要可观测的收敛证据——回滚后再次替换应可正常进入执行面（映射与清单判定不受污染） -->
- Preconditions: "回滚已成功收敛，台账新证书材料仍可用"
  fixture_spec:
    entities:
      - entity_type: "CertificateLedgerEntry"
        min_count: 1
        field_constraints:
          - field: "private key"
            value: "存在"
      - entity_type: "CloudCertMapping"
        min_count: 1
- Input: "对同一资源发起再次替换（走清单生成与执行面）"
- Output: "再次替换不受污染——清单判定与执行路径行为与首次替换一致"
- State: "映射/台账无回滚遗留脏状态阻塞后续替换"
- Side-effect: "云侧写操作（属于再次替换执行本身，非回滚遗留）"

## Journey Invariants
- 回滚不产生重复映射行，不破坏后续替换能力
- 变更单终态如实反映各产品恢复结果（成功/部分失败不美化）
