---
journey: "placeholder-fingerprint-backfill"
step: 3
step-action: "导入时解析真实指纹并登记"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/journey.md
skip_eval: true
---

# Contract: placeholder-fingerprint-backfill / Step 3: 导入时解析真实指纹并登记

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "会话处理占位条目：云适配器注册、账号 active、GetCert 返回可净化 PEM 且可解析出指纹；台账无该真实指纹"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "含占位条目 pending"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "云侧证书存在且返回仅 CERTIFICATE 块的可净化材料，解析可得出真实指纹"
        prerequisite_entity: "CloudAccount"
- Input: "会话处理占位条目：GetCert 取 PEM → 仅 CERTIFICATE 块净化 → 解析出真实指纹 → 指纹登记"
- Output: "解析成功，台账新增 fingerprint_only 记录并建立云证书映射，条目记 success 且 mappedCertId 有值"
- State: "Certificate +1（真实指纹、fingerprint_only、无私钥材料）；CloudCertMapping +1；progress.succeeded 递增"
- Side-effect: "实时 GetCert 云 API 调用"

## Outcome "parse-failure-no-backfill"
- Preconditions: "导入时点 GetCert 返回的 PEM 无法解析出指纹（或 GetCert 调用失败）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "GetCert 失败或返回材料不可解析"
        prerequisite_entity: "CloudAccount"
- Input: "会话处理该占位条目"
- Output: "条目记因失败（错误码+静态文案），绝不触发回填；失败不污染会话语义（partial_failed 既有语义），后续条目继续处理"
- State: "无台账/映射/引用回填写入；progress.failed 递增"
- Side-effect: "none"

## Outcome "no-pem-material"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_ITEM_ERROR_REASONS（discovery_import_service.go:42,238-240）：证书在库但未返回 PEM 材料（如 Azure 非证书 secret）时记 CERT_GET_FAILED: 云侧未返回可导入的证书材料，与解析失败同口径不触发回填 -->
- Preconditions: "云侧证书在库（Exists=true）但未返回可导入的证书材料（如 Azure 非证书类 secret）"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "GetCert Exists=true 但证书材料为空"
        prerequisite_entity: "CloudAccount"
- Input: "会话处理该占位条目"
- Output: "条目记因云侧未返回可导入证书材料的静态失败因；不触发回填，会话继续"
- State: "无任何写入"
- Side-effect: "none"

## Journey Invariants
- 解析失败的条目绝不触发回填
- 入库 CertPEM 仅含 CERTIFICATE 块（fingerprint_only 形态），回填一律以导入时点 GetCert 结果为准

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
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支可解析真实指纹；parse-failure 分支材料不可解析；no-pem 分支在库但无材料"
      prerequisite_entity: "CloudAccount"
```
