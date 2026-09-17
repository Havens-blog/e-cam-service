---
journey: "cloud-failure-isolation"
step: 5
step-action: "会话终态 partial_failed 与重跑收敛"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/journey.md
skip_eval: true
---

# Contract: cloud-failure-isolation / Step 5: 会话终态 partial_failed 与重跑收敛

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "partial-failed-terminal-summary"
- Preconditions: "本轮存在任一失败（枚举/判定层 Failures 非空或导入层 ImportFailed 大于 0）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_failed"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "等待会话终态，核对失败摘要"
- Output: "终态 partial_failed；失败摘要仅白名单字段（cloud/accountKey/cloudCertId 可省略 + reason）+静态 reason；无云侧错误细节"
- State: "终态二值收敛之一；失败留痕完整"
- Side-effect: "none"

## Outcome "terminal-state-binary-semantics"
<!-- source: inferred -->
<!-- reasoning: journey Step 5b + Fact Table CERT_SYNC_STATUS_RULE（cert_sync_service.go:319-323，无失败=completed/任一失败=partial_failed）+ IMPORT_STATUS_ENUM（domain/discovery_session.go:14-16） -->
- Preconditions: "分别构造全成功与含失败两轮"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "两轮分别为 completed 与 partial_failed"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "核对两轮终态"
- Output: "无失败=completed；任一失败=partial_failed；不出现中间态卡死（running 恒收敛到二值之一）"
- State: "终态可判定（status + finishedAt）"
- Side-effect: "none"

## Outcome "rerun-converges-idempotent"
<!-- source: inferred -->
<!-- reasoning: journey Step 5 expected「重跑仅处理剩余失败面并幂等收敛」+ Fact Table UK_FINGERPRINT（domain/repository.go:12-14）+ MAPPING_UNIQUE_KEY（cert_sync_service.go:30-32） -->
- Preconditions: "失败轮结束后重新触发一轮"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
      - entity_type: "Certificate"
        min_count: 1
- Input: "重跑一轮并核对收敛"
- Output: "失败面按增量判定重新处理，成功面不重复（已入账跳过）；重跑轮自身收敛（completed 或 partial_failed，失败不跨轮累积）"
- State: "台账/映射无重复行"
- Side-effect: "none"

## Journey Invariants
- 会话终态二值收敛：completed / partial_failed
- 失败可重跑幂等收敛
- errorReason 为白名单静态文案，不携带云响应片段/凭证
