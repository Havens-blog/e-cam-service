---
journey: "duplicate-concurrent-mapping"
step: 3
step-action: "首条完成指纹登记与映射建档"
generated: "2026-08-25"
sources:
  - docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/journey.md
skip_eval: true
---

# Contract: duplicate-concurrent-mapping / Step 3: 首条完成指纹登记与映射建档

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "会话处理首条：该指纹当前不在台账；云适配器注册、账号 active、云侧证书存在可净化解析"
  fixture_spec:
    entities:
      - entity_type: "DiscoveryImportSession"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running"
          - field: "items"
            value: "首条为可解析 pending 条目"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "云侧证书存在且返回可净化 PEM；台账无该指纹记录"
        prerequisite_entity: "Certificate"
- Input: "会话处理首条：GetCert → 净化 → 解析 → 指纹登记 → 映射建档"
- Output: "台账新增一条 fingerprint_only 记录（uk_fingerprint 唯一），并为本云本账号建立一条 CloudCertMapping；条目 success 且 mappedCertId 指向新记录"
- State: "Certificate +1（同指纹唯一）；CloudCertMapping +1（本云本账号）；progress.succeeded 递增"
- Side-effect: "实时 GetCert 云 API 调用"

## Outcome "duplicate-fingerprint-continues"
- Preconditions: "登记时该指纹已存在台账记录（非删除态），Create 命中 uk_fingerprint 哨兵 ErrDuplicateFingerprint"
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
            value: "与待导入证书解析指纹一致"
      - entity_type: "CloudAccount"
        min_count: 1
    state_requirements:
      - description: "既有证书存在且非删除态"
        prerequisite_entity: "Certificate"
- Input: "会话继续处理该条目：GetByFingerprint 取既有证书后 Upsert 本云本账号映射"
- Output: "不复用失败路径：映射建档成功且指向既有证书 ID（mappedCertID 正确），条目状态为 success（附已在台账补建映射说明），条目原因文案准确"
- State: "不新增 Certificate；CloudCertMapping +1（本云本账号）"
- Side-effect: "none"

## Outcome "ledger-write-failure"
<!-- source: inferred -->
<!-- reasoning: Fact Table IMPORT_ITEM_ERROR_REASONS（discovery_import_service.go:46,273-278）：Certificate 仓储 Create 非 duplicate 错误返回 INTERNAL_ERROR: 台账写入失败，逐条记因不中断会话 -->
- Preconditions: "台账写入发生非重复指纹的仓储故障（注入式错误）"
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
      - description: "Certificate 仓储 Create 注入非 duplicate 故障"
        prerequisite_entity: "Certificate"
- Input: "会话处理该条目并尝试指纹登记"
- Output: "条目记 INTERNAL_ERROR 台账写入失败的静态失败因；会话不中断，后续条目继续"
- State: "无台账/映射写入"
- Side-effect: "none"

## Journey Invariants
- 台账按指纹全局唯一（uk_fingerprint）：任何路径不得产生第二条同指纹台账记录
- 撞指纹一律转取既有证书 + 补建映射 + success，永不降级为失败条目

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
      field_constraints:
        - field: "fingerprint"
          value: "duplicate 分支与待导入证书一致"
    - entity_type: "CloudAccount"
      min_count: 1
  state_requirements:
    - description: "success 分支台账无该指纹；duplicate 分支已有同指纹；write-failure 分支仓储注入故障"
      prerequisite_entity: "Certificate"
```
