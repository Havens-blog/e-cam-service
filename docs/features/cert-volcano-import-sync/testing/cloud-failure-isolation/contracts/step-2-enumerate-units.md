---
journey: "cloud-failure-isolation"
step: 2
step-action: "枚举六云 × active 账号"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/journey.md
skip_eval: true
---

# Contract: cloud-failure-isolation / Step 2: 枚举六云 × active 账号

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "六云×账号矩阵中各云账号可读取（active）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 3
        field_constraints:
          - field: "provider"
            value: "跨多个云"
          - field: "status"
            value: "active"
      - entity_type: "CertLibraryInstance"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "CloudAccount"
- Input: "同步服务枚举全部证书可达云与 active 账号"
- Output: "逐云逐账号构成独立处理单元，互不共享失败状态"
- State: "AccountsScanned 逐单元累计"
- Side-effect: "none"

## Outcome "empty-account-skip"
<!-- source: inferred -->
<!-- reasoning: journey Step 2b + Fact Table CERT_SYNC_JUDGE_FOUR_STATES（cert_sync_service.go:354-361，空实例集不产生判定条目也不记失败） -->
- Preconditions: "某云某 active 账号下无任何证书实例"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
    state_requirements:
      - description: "该账号证书库列举返回空实例集"
        prerequisite_entity: "CloudAccount"
- Input: "同步枚举该账号"
- Output: "空枚举按跳过处理，不计失败"
- State: "无失败记录；Listed 计数不增加"
- Side-effect: "none"

## Outcome "invalid-account-isolated"
<!-- source: inferred -->
<!-- reasoning: journey Step 2c + Fact Table CERT_SYNC_STATIC_REASONS（reasonSyncListFailed，cert_sync_service.go:53,340-350）+ CERT_SYNC_LIST_PARTIAL_TOLERANCE（账号维度隔离，失败记因后继续） -->
- Preconditions: "某账号凭证失效或网络不可达"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "该账号列举调用不可达（凭证失效/网络错误）"
        prerequisite_entity: "CloudAccount"
- Input: "同步枚举该账号并列举"
- Output: "该云该账号跳过/隔离失败（CERT_LIST_FAILED 静态失败因），不中断其他云（账号维度隔离）"
- State: "Failures 增加一条该账号失败（cloud+accountKey 定位）"
- Side-effect: "云侧错误细节仅进服务层日志"

## Journey Invariants
- 单云单账号失败不中断其他云与账号
- 云凭证仅内存传递禁入日志
