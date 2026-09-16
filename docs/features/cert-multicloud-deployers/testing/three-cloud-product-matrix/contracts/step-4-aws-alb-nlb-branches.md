---
journey: "three-cloud-product-matrix"
step: 4
step-action: "AWS ALB/NLB 执行两段式（绑定 API 分支）"
generated: "2026-09-16"
sources:
  - docs/features/cert-multicloud-deployers/testing/three-cloud-product-matrix/journey.md
skip_eval: true
---

# Contract: three-cloud-product-matrix / Step 4: AWS ALB/NLB 执行两段式（绑定 API 分支）

<!-- gen-contracts: do not edit manually. Regenerate via /gen-contracts. -->

> **Note**: Contracts generated without eval-journey verification (SKIP_EVAL_GATE=true). Review with extra scrutiny.

## Outcome "alb-nlb-listener-certificates"
<!-- 事实对齐：实现中 alb 与 nlb 共用 ELBv2 AddListenerCertificates API 形态（aws/cert.go:61-68,220-221），分支显式、绑定前 DescribeListenerCertificates 幂等预检、默认证书位次不变（:354-371）。 -->
- Preconditions: "AWS alb/nlb 两组合引用就绪（监听器 ARN 资源形态）；ACM 证书已上传（ARN 可用）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 2
        field_constraints:
          - field: "product"
            value: "AWS alb 与 nlb 各一，status=pending"
      - entity_type: "CloudAccount"
        min_count: 1
        field_constraints:
          - field: "provider"
            value: "aws"
- Input: "运维人员对 AWS alb/nlb 两组合执行上传与绑定"
- Output: "上传统一走 ACM 导入返回 ARN；alb 与 nlb 按产品分支走 ELBv2 监听证书 API（共用 AddListenerCertificates 形态、绑定前监听器证书描述幂等预检、默认证书位次不变）；两组合均落 ACM ARN 映射"
- State: "两组合条目 success；两条 ARN 形态 active 映射"
- Side-effect: "ELBv2 只读预检 + 监听证书附加调用"

## Outcome "nlb-explicit-product-branch"
<!-- 事实对齐 + journey 4b 的意图：NLB 不误走 CloudFront 分配路径；分支按产品显式路由，绑定行为与 ALB 分支可区分（资源形态同为监听器 ARN，但产品语义独立）。 -->
- Preconditions: "NLB 组合的绑定执行（TLS 监听器资源形态）"
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
- Input: "系统对 NLB 组合执行绑定段"
- Output: "按产品分支走监听证书 API 完成绑定（ELBv2 形态，不误走 CloudFront 分配路径），绑定前幂等预检防重复附加；不出现调用成功实则无效的假绑定"
- State: "NLB 监听器证书集更新且默认证书位次不变"
- Side-effect: "ELBv2 API 调用"

## Outcome "listener-region-mismatch-rejected"
<!-- source: inferred -->
<!-- reasoning: Fact Table MC_AWS_LISTENER_REGION_CHECK（aws/cert.go:342-346：监听器 ARN 地域与证书 ARN 地域不一致 → 显式错误 listener region differs ... import certificate per region） -->
- Preconditions: "监听器 ARN 地域与 ACM 证书 ARN 地域不一致（证书按单地域导入）"
  fixture_spec:
    entities:
      - entity_type: "ChangeItem"
        min_count: 1
        field_constraints:
          - field: "product"
            value: "AWS alb 或 nlb"
    state_requirements:
      - description: "注入跨地域 ARN 组合（监听器与证书地域不同）"
        prerequisite_entity: "CloudAccount"
- Input: "系统执行绑定段"
- Output: "显式失败：提示监听器与证书地域不一致、需按地域导入证书，不跨地域强绑"
- State: "条目 failed（EXEC_FAILED 静态文案）；映射随补偿转 orphan"
- Side-effect: "补偿 CleanupOrphan 云调用"

## Journey Invariants
- 9 个 cloud×product 组合共用同一五方法端口语义与两段式编排，云差异只在每云适配层归一
- 云证书 ID 形态按云固定：AWS ACM ARN；形态校验失败显式报错不猜测
- 单组合失败隔离：任一组合的失败/跳过不影响其余组合的执行与终态

## Fixture Specification

This Contract requires the following pre-existing data state. See `rules/fixture-spec.md` for schema details.

```yaml
fixture_spec:
  entities:
    - entity_type: "ChangeItem"
      min_count: 2
      field_constraints:
        - field: "product"
          value: "AWS alb 与 nlb"
    - entity_type: "CloudCertMapping"
      min_count: 1
      field_constraints:
        - field: "status"
          value: "active（ACM ARN）"
    - entity_type: "CloudAccount"
      min_count: 1
```
