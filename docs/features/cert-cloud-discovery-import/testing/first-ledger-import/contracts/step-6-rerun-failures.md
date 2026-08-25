---
journey: "first-ledger-import"
step: 6
step-action: "重跑处理剩余失败项"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
skip_eval: true
---

# Contract: first-ledger-import / Step 6: 重跑处理剩余失败项

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "上一轮导入会话 partial_failed；失败条目对应的云侧问题已消除（证书可正常 GetCert 与解析）；上轮成功条目已在台账与映射中"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_failed"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "hostingStatus"
            value: "fingerprint_only"
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
    state_requirements:
      - description: "失败条目云侧证书已恢复可导入"
        prerequisite_entity: "CloudAccount"
- Input: "运维人员重新 POST /api/v1/certs/discovery/import，仅勾选上轮失败条目"
- Output: "202 新会话仅处理剩余项且幂等；已成功条目不产生重复台账记录；最终台账与映射收敛到一致状态；刷新台账页可见新登记证书及其引用关联"
- State: "失败条目经重跑登记入台账（或撞指纹走补建映射）；台账记录按指纹仍全局唯一；映射按三元组幂等收敛"
- Side-effect: "重跑会话异步执行；成功条目触发占位引用回填"

## Outcome "failed-item-still-failing"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_ITEM_ERROR_REASONS + 条目幂等语义（discovery_import_service.go:38-50）：重跑时失败条目云侧问题未消除将再次记因失败，会话仍收敛 partial_failed；重跑语义不保证转成功，验证收敛与不重复写入 -->
- Preconditions: "重跑条目对应的云侧问���未消除（如云侧证书仍不存在），条目再次失败"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "partial_failed"
    state_requirements:
      - description: "失败条目云侧问题未消除，重跑仍失败"
        prerequisite_entity: "CloudAccount"
- Input: "运维人员重跑失败条目并等待新会话终态"
- Output: "该条目再次记录静态失败因；会话收敛 partial_failed；不产生任何重复台账或映射记录"
- State: "仅失败计数变化，无新增台账记录"
- Side-effect: "none"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "CloudAccount"
- Input: "不带有效凭证 POST /api/v1/certs/discovery/import 重跑"
- Output: "401 语义的认证失败响应，不创建重跑会话"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 台账记录按指纹全局唯一（uk_fingerprint），任何重跑/并发路径不产生重复台账记录
- 重跑仅处理剩余项且幂等，最终收敛一致

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "DiscoveryImportSession"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "partial_failed"
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "hostingStatus"
          value: "fingerprint_only"
    - entity_type: "CloudCertMapping"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "Certificate"
    - entity_type: "CloudAccount"
      min_count: 1
```
