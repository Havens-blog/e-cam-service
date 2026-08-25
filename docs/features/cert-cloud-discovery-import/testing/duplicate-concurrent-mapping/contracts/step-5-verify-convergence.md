---
journey: "duplicate-concurrent-mapping"
step: 5
step-action: "验证收敛结果"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/journey.md
skip_eval: true
---

# Contract: duplicate-concurrent-mapping / Step 5: 验证收敛结果

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "多账号同证书条目组导入会话已达终态，全部条目 success"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "completed"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "同组证书唯一指纹"
      - entity_type: "CloudCertMapping"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "accountKey"
            value: "按账号各 1 条"
      - entity_type: "CertReference"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "会话终态后，运维人员刷新台账与映射数据核对"
- Output: "同指纹仅 1 条台账记录；CloudCertMapping 按账号各 1 条（两段去重）；多条引用关联到该证书；会话终态 completed（无条目降级为失败）"
- State: "无状态变更（验证读）"
- Side-effect: "none"

## Outcome "replay-idempotent"
- Preconditions: "首轮导入已成功终态，运维人员将同一批条目原样再次提交"
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
            value: "completed"
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CloudCertMapping"
        min_count: 1
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
- Input: "提交重放导入并等待终态"
- Output: "全部条目经补建映射路径记 success（附已在台账说明），幂等收敛 completed"
- State: "不产生重复台账记录与重复映射（uk_fingerprint 与 uk_fp_cloud_account 双约束下计数不变）"
- Side-effect: "none"

## Outcome "cross-cloud-same-fingerprint"
- Preconditions: "阿里云与腾讯云各有一张内容相同（同指纹）的证书被引用，两条目均可解析"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "ScanSnapshot"
        field_constraints:
          - field: "cloud"
            value: "一条 aliyun、一条 tencent，指向同一证书内容"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "一次导入勾选两云条目并等待终态"
- Output: "仅 1 条台账记录，两云各自账号各建 1 条 CloudCertMapping，引用分别关联"
- State: "Certificate 计数 1；CloudCertMapping 计数 2（按云账号区分）"
- Side-effect: "none"

## Journey Invariants
- 台账按指纹全局唯一：跨账号/跨云/重放/并发任何组合仅 1 条记录
- 映射建档后 mappedCertID 指向台账既有证书，引用关联按指纹即时生效

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
    - entity_type: "Certificate"
      min_count: 1
    - entity_type: "CloudCertMapping"
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "Certificate"
    - entity_type: "CertReference"
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "Certificate"
    - entity_type: "CloudAccount"
      min_count: 1
```
