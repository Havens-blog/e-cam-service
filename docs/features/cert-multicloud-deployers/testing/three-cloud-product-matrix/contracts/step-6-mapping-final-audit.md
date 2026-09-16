---
journey: "three-cloud-product-matrix"
step: 6
step-action: "全组合映射与清单终态核对"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/three-cloud-product-matrix/journey.md
skip_eval: true
---

# Contract: three-cloud-product-matrix / Step 6: 全组合映射与清单终态核对

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "all-combos-mapping-consistent"
- Preconditions: "9 组合执行完成（各组合条目 success，云证书 ID 均已落映射）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 9
        field_constraints:
          - field: "status"
            value: "均为 success（覆盖 9 组合）"
      - entity_type: "CloudCertMapping"
        min_count: 9
        field_constraints:
          - field: "status"
            value: "均为 active"
- Input: "运维人员核对 9 组合的 CloudCertMapping 记录与变更清单终态"
- Output: "9 组合映射齐备且形态按云正确（华为 SCM UUID / AWS ACM ARN / Azure KV secret ID 引用），按 (certFingerprint, cloud, accountKey) 唯一键各一行、状态 active；无跨云混淆；清单终态与执行结果一致"
- State: "9 条 active 映射；变更单进入验证窗口/终态收敛"
- Side-effect: "none"

## Outcome "cross-cloud-mixing-rejected"
<!-- source: inferred -->
<!-- reasoning: journey 原文"跨云混用被识别并拒绝"以实现的真实防线编码：部署器凭据云域校验（deployer_common.go:85-98，凭据 cloud 与部署器云不一致 → 拒绝）+ 映射按 cloud+accountKey+cloudCertID 三元组定位（repository/cloud_cert_mapping.go:60-76），他云 ID 不命中 -->
- Preconditions: "核对/续执行时出现跨云混用：以他云凭据调用部署器（如 AWS 凭据传入华为部署器），或以他云 ID 查询映射"
  fixture_spec:
    entities:
      - entity_type: "CloudAccount"
        min_count: 2
        field_constraints:
          - field: "provider"
            value: "不同云各一（制造错配）"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
- Input: "系统以错配凭据执行绑定/以他云 ID 写入查询映射"
- Output: "跨云混用被识别并拒绝：凭据云域不匹配显式报错；映射查询按 (cloud, accountKey, cloudCertId) 三元组定位，他云 ID 不命中；映射唯一性保持（无需云内账户级消歧）"
- State: "无脏写入，既有映射不变"
- Side-effect: "none"

## Journey Invariants
- 云证书 ID 形态按云固定：华为 SCM ID、AWS ACM ARN、Azure KV secret ID 引用；形态校验失败显式报错不猜测
- 云证书 ID 空间互斥，映射唯一性天然成立，禁止跨云混用或猜测归一
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 9
      field_constraints:
        - field: "status"
          value: "success"
    - entity_type: "CloudCertMapping"
      min_count: 9
      field_constraints:
        - field: "status"
          value: "active"
    - entity_type: "CloudAccount"
      min_count: 2
```
