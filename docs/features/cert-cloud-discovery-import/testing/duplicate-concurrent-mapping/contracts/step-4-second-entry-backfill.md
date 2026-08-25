---
journey: "duplicate-concurrent-mapping"
step: 4
step-action: "次条撞指纹转补建映射"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/journey.md
skip_eval: true
---

# Contract: duplicate-concurrent-mapping / Step 4: 次条撞指纹转补建映射

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "首条已完成指纹登记；次条为同指纹另一账号条目，登记时命中现有 uk_fingerprint 哨兵（ErrDuplicateFingerprint）；本云本账号映射尚不存在"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "含同指纹另一账号的待处理条目"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "与次条解析指纹一致（首条登记产物）"
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "accountKey"
            value: "首条账号（与次条账号不同）"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "会话处理同指纹的另一账号条目，登记时捕获 ErrDuplicateFingerprint"
- Output: "走取既有证书补建映射路径：条目记 success 并附说明文案（已在台账，已补建映射）；多账号场景不因此降级"
- State: "不新增 Certificate；CloudCertMapping 按次条账号 +1（uk_fp_cloud_account 两段去重）"
- Side-effect: "none"

## Outcome "concurrent-race-duplicate"
- Preconditions: "两个导入会话（或同会话重放与在途会话）并发处理同指纹条目，本写入者在唯一索引竞争中央败"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 2
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "两会话各含同指纹条目"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "并发写入竞争：后到者 Create 捕获 ErrDuplicateFingerprint"
        prerequisite_entity: "Certificate"
- Input: "后到者在写入时捕获 ErrDuplicateFingerprint"
- Output: "走取既有证书补建映射路径，条目记 success；不产生第二条台账记录，无失败条目"
- State: "最终仅 1 条同指纹 Certificate；映射按各自账号建立"
- Side-effect: "none"

## Outcome "account-missing"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_ITEM_ERROR_REASONS（discovery_import_service.go:44,213-216）：accountKey 未命中 active 账号时逐条记 ACCOUNT_NOT_FOUND: 云账号不存在或未启用，不中断会话、不产生映射 -->
- Preconditions: "次条目的 accountKey 不在对应云的 active 账号列表中"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "name"
            value: "与条目 accountKey 不一致（条目账号不存在）"
- Input: "会话处理该条目并解析云账号凭证"
- Output: "条目记 ACCOUNT_NOT_FOUND 云账号不存在或未启用的静态失败因；会话继续处理其余条目"
- State: "无台账/映射写入"
- Side-effect: "none"

## Journey Invariants
- 并发/重放撞指纹一律转取既有证书 + 补建映射 + success，永不降级为失败条目
- CloudCertMapping 按（指纹, 云, 账号）两段去重（uk_fp_cloud_account），同键重放幂等

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "DiscoveryImportSession"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "running"
    - entity_type: "Certificate"
      min_count: 1
    - entity_type: "CloudCertMapping"
      min_count: 1
      relationship_type: "belongs_to"
      parent_entity: "Certificate"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支首条已登记且次条账号不同；race 分支两会话并发；missing 分支条目账号不存在"
      prerequisite_entity: "Certificate"
```
