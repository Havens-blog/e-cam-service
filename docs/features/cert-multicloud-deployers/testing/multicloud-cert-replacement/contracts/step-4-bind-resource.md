---
journey: "multicloud-cert-replacement"
step: 4
step-action: "绑定段——绑定目标资源"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/multicloud-cert-replacement/journey.md
skip_eval: true
---

# Contract: multicloud-cert-replacement / Step 4: 绑定段——绑定目标资源

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "success"
- Preconditions: "上传段成功（云证书 ID 已产生且映射 active 已落库）；目标资源引用就绪（产品 × 资源 ID 形态按云约定）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "running（绑定段）"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active"
      - entity_type: "CertReference"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "华为 cdn/waf/alb/nlb、AWS cdn/alb/nlb、Azure cdn/alb 任一"
- Input: "系统执行两段式绑定段：BindResource 将云证书库中的新证书绑定到引用的目标资源"
- Output: "目标资源（华为 CDN/WAF/ALB/NLB、AWS CloudFront/ALB/NLB、Azure Front Door/Application Gateway）按各自绑定 API 完成证书绑定，绑定结果显式成功或失败（失败为静态文案 + 产品上下文，不含私钥/凭证片段）"
- State: "目标资源引用新证书；条目收敛 success"
- Side-effect: "云绑定 API 调用（含必要的前置查询与幂等预检）"

## Outcome "aws-nlb-listener-branch"
<!-- 事实对齐：实现中 AWS alb 与 nlb 共用同一 ELBv2 监听证书 API 形态（aws/cert.go:61-68,220-221），产品分支显式且绑定前经 DescribeListenerCertificates 幂等预检（:354-371），默认证书位次不变。 -->
- Preconditions: "目标资源为 AWS NLB 监听器（监听器 ARN 资源形态，TLS 监听）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "AWS nlb"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（ACM ARN）"
- Input: "系统对 NLB 引用执行绑定段"
- Output: "按产品分支走 ELBv2 监听证书 API（与 ALB 共用该 API 形态但产品路由显式），绑定前经监听器证书描述做幂等预检、默认证书位次不变；不经 CloudFront 分配路径，绑定结果可区分产品语义"
- State: "NLB 监听器证书集新增目标证书且原默认证书保留"
- Side-effect: "ELBv2 只读预检 + 监听证书附加调用"

## Outcome "azure-appgw-kv-reference"
- Preconditions: "目标资源为 Azure Application Gateway 监听器，其 SSL 证书资源为 KV-backed 形态（非 inline data 形态）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "Azure alb"
      - entity_type: "CloudCertMapping"
        min_count: 1
        field_constraints:
          - field: "status"
            value: "active（KV secret ID 引用）"
- Input: "系统对 App Gateway 引用执行绑定段"
- Output: "按 KV 证书引用形态完成绑定：以 keyVaultSecretId 指向本次上传的 KV secret（引用而非直传 ID）；inline data 形态证书资源被显式拒绝不猜测；引用已等于目标时幂等跳过"
- State: "App Gateway 监听器 SSL 证书资源指向本次上传证书的 KV 引用"
- Side-effect: "ARM 子资源写入调用"

## Journey Invariants
- 两段式顺序不可逆：UploadCert 成功前不得 BindResource；绑定失败必经 CleanupOrphan 补偿，不允许跳过补偿直接重试
- 9 个 cloud×product 组合共用同一五方法端口语义与两段式编排，云差异只在每云适配层归一
- 云端错误细节不进 API 响应仅入日志（对外为静态文案）

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 1
      field_constraints:
        - field: "product"
          value: "success: 任一注册组合；nlb 分支: AWS nlb；appgw 分支: Azure alb"
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "active"
    - entity_type: "CertReference"
      min_count: 1
```
