---
journey: "placeholder-fingerprint-backfill"
step: 5
step-action: "核对台账引用列表"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/journey.md
skip_eval: true
---

# Contract: placeholder-fingerprint-backfill / Step 5: 核对台账引用列表

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "占位条目导入成功且回填已完成；新登记证书在台账可查"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "hostingStatus"
            value: "fingerprint_only"
      - entity_type: "CertReference"
        min_count: 2
        relationship_type: "belongs_to"
        parent_entity: "Certificate"
        field_constraints:
          - field: "certFingerprint"
            value: "一条为回填后的原占位引用，一条为真实指纹引用"
- Input: "运维人员打开新登记证书的台账详情查看引用列表"
- Output: "引用列表非空——回填后的占位引用与真实指纹引用均关联到该证书（四云引用关联即时生效）；华为云与不可解析占位引用不在本路径"
- State: "无状态变更（验证读）"
- Side-effect: "none"

## Outcome "partial-failure-rescan-refresh"
- Preconditions: "首轮导入部分占位条目解析失败未回填，快照已陈旧；已完成一次新的引用扫描"
  fixture_spec:
    entities:
      - entity_type: "ScanSnapshot"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "done"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "certFingerprint"
            value: "重扫按原口径重建的占位公式派生值"
      - entity_type: "CloudAccount"
        min_count: 1
- Input: "运维人员重扫后重跑导入（对剩余占位条目）"
- Output: "重扫按原口径重建占位引用，重跑导入对剩余条目幂等处理，回填最终收敛"
- State: "剩余占位引用经重跑成功回填；已回填部分不受影响"
- Side-effect: "重跑会话异步执行"

## Outcome "unauthorized"
- Preconditions: "请求未携带有效登录会话（未认证）"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
    state_requirements:
      - description: "请求方未认证"
        prerequisite_entity: "Certificate"
- Input: "不带有效凭证请求证书详情引用列表"
- Output: "401 语义的认证失败响应，不返回引用数据"
- State: "无状态变更"
- Side-effect: "none"

## Journey Invariants
- 回填后引用按指纹即时关联生效；解析失败条目绝不触发回填（重扫重建后可重跑收敛）

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "Certificate"
      min_count: 1
      field_constraints:
        - field: "hostingStatus"
          value: "fingerprint_only"
    - entity_type: "CertReference"
      min_count: 2
      relationship_type: "belongs_to"
      parent_entity: "Certificate"
    - entity_type: "ScanSnapshot"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "done"
    - entity_type: "CloudAccount"
      min_count: 1
```
