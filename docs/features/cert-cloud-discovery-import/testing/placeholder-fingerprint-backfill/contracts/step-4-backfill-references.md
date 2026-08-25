---
journey: "placeholder-fingerprint-backfill"
step: 4
step-action: "触发占位引用批量回填"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/journey.md
skip_eval: true
---

# Contract: placeholder-fingerprint-backfill / Step 4: 触发占位引用批量回填

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "该条目导入已成功（真实指纹已解析登记）；cert_references 中仍存在匹配该三元组、指纹为占位公式派生值的引用"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
      - entity_type: "Certificate"
        min_count: 1
        field_constraints:
          - field: "fingerprint"
            value: "导入时点解析的真实指纹"
      - entity_type: "CertReference"
        min_count: 2
        field_constraints:
          - field: "certFingerprint"
            value: "占位公式派生值"
          - field: "cloud"
            value: "与条目三元组一致"
          - field: "accountKey"
            value: "与条目三元组一致"
          - field: "cloudCertId"
            value: "与条目三元组一致"
- Input: "条目成功后，会话按三元组将仍为占位指纹的引用批量回填为真实指纹"
- Output: "匹配三元组的占位引用全部回填为导入时点 GetCert 解析的真实指纹；回填后引用按指纹即时关联生效"
- State: "cert_references 中该三元组的占位指纹批量更新为真实指纹；真实指纹引用不受影响"
- Side-effect: "批量回填写（best-effort 补偿语义，失败仅记日志不失败条目）"

## Outcome "real-fingerprint-never-overwritten"
- Preconditions: "同一三元组下既有占位指纹引用又有真实（非占位）指纹引用"
  fixture_spec:
    entities:
      - entity_type: "Certificate"
        min_count: 1
      - entity_type: "CertReference"
        min_count: 2
        field_constraints:
          - field: "certFingerprint"
            value: "一条为占位公式派生值，一条为真实指纹"
- Input: "会话执行回填"
- Output: "仅占位指纹引用被回填；真实指纹引用永不被回填覆盖（续期漂移只留下可由重扫刷新的覆盖率缺口）"
- State: "真实指纹引用保持原值不变"
- Side-effect: "none"

## Outcome "acm-renewal-current-cert"
- Preconditions: "AWS ACM 证书续期后保留同一证书 ID/ARN 但内容已更换，扫描时点引用为占位指纹"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "certFingerprint"
            value: "占位公式派生值"
          - field: "cloud"
            value: "aws"
    state_requirements:
      - description: "导入时点 GetCert 返回续期后的现行证书内容"
        prerequisite_entity: "CloudAccount"
- Input: "导入时点执行回填"
- Output: "回填的是导入时点 GetCert 得到的现行证书指纹（非扫描时点旧内容），符合回填一律以导入时点为准语义"
- State: "引用回填为现行证书指纹"
- Side-effect: "none"

## Outcome "wrong-backfill-rescan-recoverable"
- Preconditions: "假设回填写入了错误指纹（回填值与真实证书不符）"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "certFingerprint"
            value: "被误回填的错误指纹值"
- Input: "触发一次新的引用扫描"
- Output: "占位指纹是确定性可重算值（按公式由引用三元组重得），重扫按原口径重建占位引用，误回填可恢复"
- State: "重扫后该引用恢复为占位公式派生值，可再次经导入回填"
- Side-effect: "重扫异步编排启动"

## Outcome "multi-account-scoped"
- Preconditions: "同一 cloudCertId 被多个账号的引用以占位指纹引用（各自三元组不同），各账号条目分别导入成功"
  fixture_spec:
    entities:
      - entity_type: "CertReference"
        min_count: 2
        field_constraints:
          - field: "certFingerprint"
            value: "占位公式派生值"
          - field: "accountKey"
            value: "分属不同账号（三元组各异）"
      - entity_type: "CloudAccount"
        min_count: 2
- Input: "各账号条目分别导入成功并触发各自回填"
- Output: "每个成功的三元组各自触发批量回填，仅命中本三元组的占位引用被回填，不跨账号误写"
- State: "各账号引用仅在对应条目成功后回填"
- Side-effect: "none"

## Journey Invariants
- 非占位（真实）指纹引用永不被回填覆盖
- 回填仅由导入会话按条目成功事件承担：不动扫描编排、不改引用表结构
- 回填一律以导入时点 GetCert 结果为准（现行证书口径）
- 解析失败的条目绝不触发回填

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
    - entity_type: "CertReference"
      min_count: 2
      field_constraints:
        - field: "certFingerprint"
          value: "占位公式派生值 / 真实指纹 / 误回填值（按分支）"
    - entity_type: "CloudAccount"
      min_count: 1
```
