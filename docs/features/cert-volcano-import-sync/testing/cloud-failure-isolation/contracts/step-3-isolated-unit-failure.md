---
journey: "cloud-failure-isolation"
step: 3
step-action: "单云单账号 List 失败"
generated: "2026-09-17"
sources:
  - docs/features/cert-volcano-import-sync/testing/cloud-failure-isolation/journey.md
skip_eval: true
---

# Contract: cloud-failure-isolation / Step 3: 单云单账号 List 失败

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "list-failed-isolated-static-reason"
- Preconditions: "某云某账号账号读取成功但云 API 列举失败（含限流类错误）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "失败单元与其余正常单元分属不同账号"
    state_requirements:
      - description: "失败单元列举调用返回错误，其余单元正常"
        prerequisite_entity: "CloudAccount"
- Input: "处理该云×账号单元"
- Output: "该云该账号隔离失败：失败因记 CERT_LIST_FAILED 静态文案（限流同口径，不做重试风暴——单轮不反复重试同一单元）；云侧错误细节不进摘要仅进日志；其余单元不受影响"
- State: "Failures 增加一条（cloud+accountKey 定位）；已获取部分实例仍进入判定（适配器部分结果容错）"
- Side-effect: "云侧错误细节仅 slog 日志（云凭证与响应片段不入失败记录）"

## Outcome "account-load-failed-cloud-level"
<!-- source: inferred -->
<!-- reasoning: Fact Table CERT_SYNC_STATIC_REASONS（reasonSyncAccountFailed「ACCOUNT_LOAD_FAILED: 云账号读取失败」，cert_sync_service.go:52,281-287，云级失败 AccountKey 为空） -->
- Preconditions: "某云 active 账号读取失败（仓储层错误，账号维度不可用）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "该云账号读取通道异常（ActiveByCloud 返回错误）"
        prerequisite_entity: "CloudAccount"
- Input: "处理该云枚举"
- Output: "云级失败记 ACCOUNT_LOAD_FAILED 静态失败因（AccountKey 为空=云级聚合）；该云跳过，其余云正常"
- State: "Failures 增加一条云级失败；CloudsScanned 不计入该云"
- Side-effect: "none"

## Journey Invariants
- 单云单账号失败不中断其他云与账号
- errorReason 为白名单静态文案，不携带云响应片段/凭证
