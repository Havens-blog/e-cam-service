---
journey: "three-cloud-product-matrix"
step: 3
step-action: "AWS CloudFront 执行两段式（us-east-1 约束）"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/three-cloud-product-matrix/journey.md
skip_eval: true
---

# Contract: three-cloud-product-matrix / Step 3: AWS CloudFront 执行两段式（us-east-1 约束）

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "cloudfront-upload-useast1"
- Preconditions: "AWS cdn（CloudFront）组合引用就绪（分配 ID 资源形态）；ACM 可达"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "AWS cdn（CloudFront）"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "aws"
- Input: "运维人员对 AWS cdn（CloudFront）组合执行上传与绑定"
- Output: "证书经 ACM 导入固定上传至 us-east-1 地域（CloudFront 查看器证书硬约束，上传地域与账户默认区域解耦），返回 ACM ARN；分配按查看器证书替换（SNI-only 支持方式、保留别名配置），ARN 形态写入映射"
- State: "条目 success；ARN 形态 active 映射"
- Side-effect: "ACM 上传 + CloudFront 分配更新调用（带 ETag 乐观锁）"

## Outcome "default-region-independent"
- Preconditions: "AWS 账号默认区域非 us-east-1（凭据未携带区域指定）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "AWS cdn（CloudFront）"
    state_requirements:
      - description: "AWS 账号默认区域配置为非 us-east-1"
        prerequisite_entity: "CloudAccount"
- Input: "系统对 CloudFront 组合执行上传段"
- Output: "上传地域仍恒为 us-east-1（不依赖账户默认区域）；分配绑定校验证书 ARN 地域必须为 us-east-1，跨地域显式拒绝（非 us-east-1 地域提示）"
- State: "ARN 落映射；无跨地域脏绑定"
- Side-effect: "ACM 上传调用（us-east-1 端点）"

## Outcome "cloudfront-rebind-idempotent"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_CLOUDFRONT_IDEMPOTENT（aws/cert.go:272-278：ViewerCertificate.ACMCertificateArn 已等于目标 ARN → 幂等跳过，不再消耗分配 ETag） -->
- Preconditions: "CloudFront 分配当前查看器证书已指向目标 ARN（重复绑定）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "AWS cdn（CloudFront）"
    state_requirements:
      - description: "分配查看器证书已等于目标证书 ARN"
        prerequisite_entity: "CloudAccount"
- Input: "系统重复执行绑定段"
- Output: "幂等跳过：不再发起分配更新（不消耗 ETag、无多余写调用），结果与首次绑定一致"
- State: "分配配置不变"
- Side-effect: "仅分配描述只读预检"

## Journey Invariants
- CloudFront 组合上传地域恒为 us-east-1，不受账户默认区域影响
- 云证书 ID 形态按云固定：AWS ACM ARN；形态校验失败显式报错不猜测
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "product"
          value: "AWS cdn（CloudFront）"
    - entity_type: "CloudAccount"
      min_count: 1
      field_constraints:
        - field: "provider"
          value: "aws"
```
